param(
    [Parameter(Mandatory = $true)][string]$PanelUrl,
    [string]$BasicUser = "",
    [string]$BasicPass = "",
    [string]$NodeHost = "staging-smoke.example.com",
    [switch]$SkipCleanup
)

$ErrorActionPreference = "Stop"

$base = $PanelUrl.TrimEnd("/")
$headers = @{}
if ($BasicUser -ne "" -or $BasicPass -ne "") {
    $raw = "${BasicUser}:${BasicPass}"
    $encoded = [Convert]::ToBase64String([Text.Encoding]::ASCII.GetBytes($raw))
    $headers["Authorization"] = "Basic $encoded"
}

$createdUser = $null
$createdNode = $null

function ConvertTo-SmokeJson {
    param([Parameter(Mandatory = $true)]$Value)
    return $Value | ConvertTo-Json -Depth 16 -Compress
}

function Invoke-SmokeJson {
    param(
        [Parameter(Mandatory = $true)][string]$Method,
        [Parameter(Mandatory = $true)][string]$Path,
        $Body = $null,
        [hashtable]$ExtraHeaders = @{}
    )

    $requestHeaders = @{}
    foreach ($key in $headers.Keys) {
        $requestHeaders[$key] = $headers[$key]
    }
    foreach ($key in $ExtraHeaders.Keys) {
        $requestHeaders[$key] = $ExtraHeaders[$key]
    }

    $args = @{
        Uri = "$base$Path"
        Method = $Method
        Headers = $requestHeaders
        TimeoutSec = 20
    }
    if ($null -ne $Body) {
        $args["ContentType"] = "application/json"
        $args["Body"] = ConvertTo-SmokeJson $Body
    }
    return Invoke-RestMethod @args
}

function Invoke-SmokeDelete {
    param([Parameter(Mandatory = $true)][string]$Path)

    try {
        Invoke-WebRequest -Uri "$base$Path" -Method Delete -Headers $headers -TimeoutSec 20 -UseBasicParsing | Out-Null
    } catch {
        Write-Warning "cleanup failed for ${Path}: $($_.Exception.Message)"
    }
}

function Assert-Smoke {
    param(
        [Parameter(Mandatory = $true)][bool]$Condition,
        [Parameter(Mandatory = $true)][string]$Message
    )
    if (!$Condition) {
        throw $Message
    }
}

try {
    Write-Host "Checking panel health..."
    $health = Invoke-SmokeJson -Method Get -Path "/api/health"
    Assert-Smoke ($health.status -eq "ok") "health check did not return ok"

    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $padding = "stop=2`n0=10-20`n1=30-40"

    Write-Host "Creating temporary smoke user..."
    $createdUser = Invoke-SmokeJson -Method Post -Path "/api/users" -Body @{
        name = "staging-smoke-$stamp"
        quota_bytes = 0
    }
    Assert-Smoke ($createdUser.id -ne "" -and $createdUser.subscription_token -ne "") "user was not initialized"
    Assert-Smoke ($createdUser.ss_2022_password_32 -ne "") "user 2022 key was not initialized"

    Write-Host "Creating temporary smoke node..."
    $createdNode = Invoke-SmokeJson -Method Post -Path "/api/exit-nodes" -Body @{
        name = "Staging Smoke $stamp"
        hostname = $NodeHost
        enabled = $true
        anytls_enabled = $true
        ss_enabled = $true
        anytls_port = 2443
        anytls_padding_scheme = $padding
        ss_port = 8388
        ss_method = "2022-blake3-chacha20-poly1305"
        cert_mode = "manual"
        certificate_path = "/etc/sing-box/smoke-cert.pem"
        key_path = "/etc/sing-box/smoke-key.pem"
        stats_mode = "mock"
    }
    Assert-Smoke ($createdNode.id -ne "" -and $createdNode.agent_token -ne "") "node was not initialized"

    Write-Host "Checking generated subscription..."
    $subscription = Invoke-SmokeJson -Method Get -Path "/sub/$($createdUser.subscription_token)"
    $ssOutbound = @($subscription.outbounds) | Where-Object { $_.type -eq "shadowsocks" } | Select-Object -First 1
    Assert-Smoke ($null -ne $ssOutbound) "subscription did not include Shadowsocks outbound"
    Assert-Smoke ($ssOutbound.method -eq "2022-blake3-chacha20-poly1305") "subscription used unexpected SS method: $($ssOutbound.method)"
    Assert-Smoke ($ssOutbound.password -like "*:*") "2022 subscription password did not combine server and user keys"
    Assert-Smoke ($ssOutbound.multiplex.enabled -eq $true) "subscription Shadowsocks multiplex was not enabled"

    Write-Host "Checking desired config..."
    $desired = Invoke-SmokeJson -Method Get -Path "/api/agent/$($createdNode.id)/desired-config" -ExtraHeaders @{
        "X-Sing-Panel-Agent-Token" = $createdNode.agent_token
    }
    $inbounds = @($desired.sing_box_config.inbounds)
    $anytlsInbound = $inbounds | Where-Object { $_.type -eq "anytls" } | Select-Object -First 1
    $ssInbound = $inbounds | Where-Object { $_.type -eq "shadowsocks" } | Select-Object -First 1
    Assert-Smoke ($null -ne $anytlsInbound) "desired config did not include AnyTLS inbound"
    Assert-Smoke ($null -ne $ssInbound) "desired config did not include Shadowsocks inbound"
    Assert-Smoke ((@($anytlsInbound.padding_scheme) -join "|") -eq "stop=2|0=10-20|1=30-40") "desired config did not use custom AnyTLS padding"
    Assert-Smoke ($ssInbound.method -eq "2022-blake3-chacha20-poly1305") "desired config used unexpected SS method: $($ssInbound.method)"
    Assert-Smoke ($ssInbound.multiplex.enabled -eq $true) "desired config Shadowsocks multiplex was not enabled"

    Write-Host "Checking patch/version flow..."
    $patched = Invoke-SmokeJson -Method Patch -Path "/api/exit-nodes/$($createdNode.id)" -Body @{
        anytls_padding_scheme = "stop=1`n0=20-30"
        ss_method = "aes-256-gcm"
    }
    Assert-Smoke ($patched.expected_config_version -gt $createdNode.expected_config_version) "patch did not bump desired config version"
    $desiredAfterPatch = Invoke-SmokeJson -Method Get -Path "/api/agent/$($createdNode.id)/desired-config" -ExtraHeaders @{
        "X-Sing-Panel-Agent-Token" = $patched.agent_token
    }
    $patchedSSInbound = @($desiredAfterPatch.sing_box_config.inbounds) | Where-Object { $_.type -eq "shadowsocks" } | Select-Object -First 1
    Assert-Smoke ($patchedSSInbound.method -eq "aes-256-gcm") "patched desired config did not switch SS method"

    Write-Host "Checking heartbeat/applied status..."
    Invoke-SmokeJson -Method Post -Path "/api/agent/$($createdNode.id)/heartbeat" -ExtraHeaders @{
        "X-Sing-Panel-Agent-Token" = $patched.agent_token
    } -Body @{
        applied_config_version = $desiredAfterPatch.version
    } | Out-Null
    $nodesAfterHeartbeat = @(Invoke-SmokeJson -Method Get -Path "/api/exit-nodes")
    $nodeAfterHeartbeat = $nodesAfterHeartbeat | Where-Object { $_.id -eq $createdNode.id } | Select-Object -First 1
    Assert-Smoke ($nodeAfterHeartbeat.applied_config_version -eq $desiredAfterPatch.version) "heartbeat did not update applied config version"

    Write-Host "Checking pause flow..."
    $paused = Invoke-SmokeJson -Method Patch -Path "/api/exit-nodes/$($createdNode.id)" -Body @{
        enabled = $false
    }
    $pausedDesired = Invoke-SmokeJson -Method Get -Path "/api/agent/$($createdNode.id)/desired-config" -ExtraHeaders @{
        "X-Sing-Panel-Agent-Token" = $paused.agent_token
    }
    Assert-Smoke ($pausedDesired.paused -eq $true) "paused desired config did not mark paused=true"
    Assert-Smoke (@($pausedDesired.sing_box_config.inbounds).Count -eq 0) "paused desired config still had inbounds"

    $result = [pscustomobject]@{
        ok = $true
        panel = $base
        smoke_user_id = $createdUser.id
        smoke_node_id = $createdNode.id
        desired_version = $pausedDesired.version
        cleanup = !$SkipCleanup
    }
    Write-Host "Staging panel smoke passed."
    $result | ConvertTo-Json -Compress
} finally {
    if (!$SkipCleanup) {
        if ($null -ne $createdUser -and $createdUser.id) {
            Write-Host "Cleaning up temporary user..."
            Invoke-SmokeDelete -Path "/api/users/$($createdUser.id)"
        }
        if ($null -ne $createdNode -and $createdNode.id) {
            Write-Host "Cleaning up temporary node..."
            Invoke-SmokeDelete -Path "/api/exit-nodes/$($createdNode.id)"
        }
    }
}
