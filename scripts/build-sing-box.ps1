param(
    [Parameter(Mandatory = $true)]
    [string]$SingBoxSource,

    [string]$Output = "",
    [string[]]$ExtraTags = @("with_v2ray_api")
)

$ErrorActionPreference = "Stop"

$sourcePath = Resolve-Path -LiteralPath $SingBoxSource
$releaseDir = Join-Path $sourcePath "release"
$isWindowsHost = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)
$isLinuxHost = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Linux)
$isMacHost = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::OSX)

if ($isWindowsHost) {
    $defaultTagsFile = Join-Path $releaseDir "DEFAULT_BUILD_TAGS_WINDOWS"
    if ($Output -eq "") {
        $Output = Join-Path (Get-Location) "sing-box.exe"
    }
} elseif ($isLinuxHost -or $isMacHost) {
    $defaultTagsFile = Join-Path $releaseDir "DEFAULT_BUILD_TAGS"
    if ($Output -eq "") {
        $Output = Join-Path (Get-Location) "sing-box"
    }
} else {
    $defaultTagsFile = Join-Path $releaseDir "DEFAULT_BUILD_TAGS_OTHERS"
    if ($Output -eq "") {
        $Output = Join-Path (Get-Location) "sing-box"
    }
}

$ldflagsFile = Join-Path $releaseDir "LDFLAGS"
if (!(Test-Path -LiteralPath $defaultTagsFile)) {
    throw "Missing official default tag file: $defaultTagsFile"
}
if (!(Test-Path -LiteralPath $ldflagsFile)) {
    throw "Missing official linker flags file: $ldflagsFile"
}

$tags = [System.Collections.Generic.List[string]]::new()
foreach ($tag in ((Get-Content -LiteralPath $defaultTagsFile -Raw) -split "\s+")) {
    $trimmed = $tag.Trim()
    if ($trimmed -and !$tags.Contains($trimmed)) {
        $tags.Add($trimmed)
    }
}
foreach ($tag in $ExtraTags) {
    $trimmed = $tag.Trim()
    if ($trimmed -and !$tags.Contains($trimmed)) {
        $tags.Add($trimmed)
    }
}

$ldflags = (Get-Content -LiteralPath $ldflagsFile -Raw).Trim()
$tagString = [string]::Join(" ", $tags)
$outputParent = Split-Path -Parent $Output
if ($outputParent -and !(Test-Path -LiteralPath $outputParent)) {
    New-Item -ItemType Directory -Path $outputParent | Out-Null
}

Write-Host "Building sing-box from $sourcePath"
Write-Host "Tags: $tagString"
Write-Host "Ldflags: $ldflags"
Write-Host "Output: $Output"

Push-Location $sourcePath
try {
    & go build -trimpath -tags $tagString -ldflags $ldflags -o $Output ./cmd/sing-box
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
