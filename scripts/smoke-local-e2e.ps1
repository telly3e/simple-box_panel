param(
    [string]$SingBoxBin = "",
    [string]$RunDir = "",
    [int]$ApiPort = 18080,
    [int]$HttpPort = 18081,
    [int]$StatsPort = 19085,
    [int]$ServerSSPort = 19088,
    [int]$ClientMixedPort = 19090,
    [string]$GoCache = ""
)

$ErrorActionPreference = "Stop"

$repo = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
if ($RunDir -eq "") {
    $RunDir = Join-Path $repo ".runtime\local-e2e"
}
if (![System.IO.Path]::IsPathRooted($RunDir)) {
    $RunDir = Join-Path (Get-Location) $RunDir
}
$RunDir = [System.IO.Path]::GetFullPath($RunDir)

if ($SingBoxBin -eq "") {
    $candidate = Join-Path $repo ".runtime\bin\sing-box.exe"
    if (Test-Path -LiteralPath $candidate) {
        $SingBoxBin = $candidate
    } else {
        $SingBoxBin = "sing-box"
    }
}
if ($SingBoxBin -ne "sing-box" -and ![System.IO.Path]::IsPathRooted($SingBoxBin)) {
    $SingBoxBin = Join-Path (Get-Location) $SingBoxBin
}

if ($GoCache -eq "") {
    $GoCache = Join-Path $repo ".runtime\go-build"
}
if (![System.IO.Path]::IsPathRooted($GoCache)) {
    $GoCache = Join-Path (Get-Location) $GoCache
}

$binDir = Join-Path $RunDir "bin"
$agentDir = Join-Path $RunDir "agent"
$wwwDir = Join-Path $RunDir "www"
$db = Join-Path $RunDir "panel-e2e.db"
$apiExe = Join-Path $binDir "sing-panel-api.exe"
$agentExe = Join-Path $binDir "sing-panel-agent.exe"
$clientConfig = Join-Path $RunDir "client.json"

New-Item -ItemType Directory -Force $RunDir, $binDir, $agentDir, $wwwDir, $GoCache | Out-Null
Set-Content -LiteralPath (Join-Path $wwwDir "ping.txt") -Value "sing-panel-local-e2e-ok"
Remove-Item -LiteralPath $db, "$db-shm", "$db-wal" -Force -ErrorAction SilentlyContinue

$oldGoCache = $env:GOCACHE
$env:GOCACHE = $GoCache
$procs = @()

function Start-SmokeProcess {
    param(
        [Parameter(Mandatory = $true)][string]$File,
        [Parameter(Mandatory = $true)][string]$Arguments
    )

    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $File
    $psi.Arguments = $Arguments
    $psi.WorkingDirectory = $repo
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    return [System.Diagnostics.Process]::Start($psi)
}

function Wait-SmokeHttp {
    param(
        [Parameter(Mandatory = $true)][string]$Url,
        [int]$Seconds = 10
    )

    $deadline = (Get-Date).AddSeconds($Seconds)
    do {
        try {
            return Invoke-RestMethod $Url -TimeoutSec 2
        } catch {
            Start-Sleep -Milliseconds 300
        }
    } while ((Get-Date) -lt $deadline)
    throw "timeout waiting for $Url"
}

try {
    Write-Host "Building API and agent..."
    Push-Location $repo
    try {
        & go build -o $apiExe .\apps\api
        if ($LASTEXITCODE -ne 0) { throw "go build apps/api failed with exit code $LASTEXITCODE" }
        & go build -o $agentExe .\apps\agent
        if ($LASTEXITCODE -ne 0) { throw "go build apps/agent failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }

    Write-Host "Starting panel API on 127.0.0.1:$ApiPort..."
    $api = Start-SmokeProcess $apiExe "--addr 127.0.0.1:$ApiPort --db `"$db`""
    $procs += $api
    Wait-SmokeHttp "http://127.0.0.1:$ApiPort/api/health" | Out-Null

    Write-Host "Creating smoke user and Exit node..."
    $user = Invoke-RestMethod "http://127.0.0.1:$ApiPort/api/users" -Method Post -ContentType "application/json" -Body (@{
        name = "local-smoke"
        quota_bytes = 0
    } | ConvertTo-Json -Compress)
    $node = Invoke-RestMethod "http://127.0.0.1:$ApiPort/api/exit-nodes" -Method Post -ContentType "application/json" -Body (@{
        name = "Local SS Exit"
        hostname = "127.0.0.1"
        anytls_enabled = $false
        ss_enabled = $true
        ss_port = $ServerSSPort
        stats_mode = "v2ray-api"
        stats_api_listen = "127.0.0.1:$StatsPort"
        cert_mode = "manual"
    } | ConvertTo-Json -Compress)

    Write-Host "Running agent once to write and validate server config..."
    & $agentExe --api-url "http://127.0.0.1:$ApiPort" --node-id $node.id --agent-token $node.agent_token --runtime-dir $agentDir --stats-mode auto --check-config --sing-box-bin $SingBoxBin --apply-only --once | Out-Host
    $serverConfig = Join-Path $agentDir "$($node.id)\sing-box.json"
    if (!(Test-Path -LiteralPath $serverConfig)) {
        throw "agent did not write $serverConfig"
    }

    Write-Host "Starting sing-box server..."
    $server = Start-SmokeProcess $SingBoxBin "run -c `"$serverConfig`""
    $procs += $server
    Start-Sleep -Seconds 2
    if ($server.HasExited) {
        throw "sing-box server exited with code $($server.ExitCode)"
    }

    Write-Host "Starting local HTTP origin..."
    $http = Start-SmokeProcess "python" "-m http.server $HttpPort --bind 127.0.0.1 --directory `"$wwwDir`""
    $procs += $http
    Wait-SmokeHttp "http://127.0.0.1:$HttpPort/ping.txt" | Out-Null

    Write-Host "Writing and validating sing-box client config..."
    $clientObj = [ordered]@{
        log = @{ level = "info" }
        inbounds = @(@{
            type = "mixed"
            tag = "mixed-in"
            listen = "127.0.0.1"
            listen_port = $ClientMixedPort
        })
        outbounds = @(
            @{
                type = "shadowsocks"
                tag = "proxy"
                server = "127.0.0.1"
                server_port = $ServerSSPort
                method = "aes-128-gcm"
                password = $user.ss_password
            },
            @{ type = "direct"; tag = "direct" }
        )
        route = @{ final = "proxy" }
    }
    [System.IO.File]::WriteAllText($clientConfig, ($clientObj | ConvertTo-Json -Depth 8), [System.Text.UTF8Encoding]::new($false))
    & $SingBoxBin check -c $clientConfig | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "sing-box client config check failed with exit code $LASTEXITCODE" }

    Write-Host "Starting sing-box client on mixed port $ClientMixedPort..."
    $client = Start-SmokeProcess $SingBoxBin "run -c `"$clientConfig`""
    $procs += $client
    Start-Sleep -Seconds 2
    if ($client.HasExited) {
        throw "sing-box client exited with code $($client.ExitCode)"
    }

    Write-Host "Sending HTTP request through local sing-box client/server..."
    $curlOut = & curl.exe --silent --show-error --socks5-hostname "127.0.0.1:$ClientMixedPort" "http://127.0.0.1:$HttpPort/ping.txt"
    if ($LASTEXITCODE -ne 0) { throw "curl failed with exit code $LASTEXITCODE" }
    if ($curlOut.Trim() -ne "sing-panel-local-e2e-ok") {
        throw "unexpected curl output: $curlOut"
    }

    Write-Host "Running agent again to collect V2Ray API stats..."
    & $agentExe --api-url "http://127.0.0.1:$ApiPort" --node-id $node.id --agent-token $node.agent_token --runtime-dir $agentDir --stats-mode auto --check-config --sing-box-bin $SingBoxBin --once | Out-Host

    $updatedUser = @(Invoke-RestMethod "http://127.0.0.1:$ApiPort/api/users") | Where-Object { $_.id -eq $user.id } | Select-Object -First 1
    $updatedNode = @(Invoke-RestMethod "http://127.0.0.1:$ApiPort/api/exit-nodes") | Where-Object { $_.id -eq $node.id } | Select-Object -First 1
    if ($updatedUser.used_bytes -le 0) {
        throw "expected used_bytes > 0, got $($updatedUser.used_bytes)"
    }
    if ($updatedNode.applied_config_version -ne $updatedNode.expected_config_version) {
        throw "expected applied version to match desired: applied=$($updatedNode.applied_config_version) desired=$($updatedNode.expected_config_version)"
    }

    $result = [pscustomobject]@{
        ok = $true
        user_id = $updatedUser.id
        used_bytes = $updatedUser.used_bytes
        node_id = $updatedNode.id
        expected_config_version = $updatedNode.expected_config_version
        applied_config_version = $updatedNode.applied_config_version
        last_agent_error = $updatedNode.last_agent_error
        server_config = $serverConfig
        client_config = $clientConfig
        run_dir = $RunDir
    }
    Write-Host "Smoke passed."
    $result | ConvertTo-Json -Compress
} finally {
    foreach ($p in $procs) {
        try {
            if ($p -and !$p.HasExited) {
                $p.Kill($true)
                $p.WaitForExit(3000) | Out-Null
            }
        } catch {}
    }
    $env:GOCACHE = $oldGoCache
}
