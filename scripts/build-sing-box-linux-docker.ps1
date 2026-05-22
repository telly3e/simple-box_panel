param(
    [Parameter(Mandatory = $true)]
    [string]$SingBoxSource,

    [Parameter(Mandatory = $true)]
    [string]$Output,

    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64",

    [string]$GoImage = "golang:1.25.5-bookworm",
    [string[]]$ExtraTags = @("with_v2ray_api", "with_acme"),
    [string[]]$ExcludeTags = @("with_naive_outbound")
)

$ErrorActionPreference = "Stop"

$sourcePath = (Resolve-Path -LiteralPath $SingBoxSource).Path
$callerPath = Get-Location
if (![System.IO.Path]::IsPathRooted($Output)) {
    $Output = Join-Path $callerPath $Output
}
$outputParent = Split-Path -Parent $Output
if ($outputParent -and !(Test-Path -LiteralPath $outputParent)) {
    New-Item -ItemType Directory -Path $outputParent | Out-Null
}

$sourceDockerPath = $sourcePath.Replace("\", "/")
$outputParentDockerPath = (Resolve-Path -LiteralPath $outputParent).Path.Replace("\", "/")
$outputName = Split-Path -Leaf $Output
$defaultTagsFile = Join-Path $sourcePath "release\DEFAULT_BUILD_TAGS"
$ldflagsFile = Join-Path $sourcePath "release\LDFLAGS"
if (!(Test-Path -LiteralPath $defaultTagsFile)) {
    throw "Missing official default tag file: $defaultTagsFile"
}
if (!(Test-Path -LiteralPath $ldflagsFile)) {
    throw "Missing official linker flags file: $ldflagsFile"
}

$tags = [System.Collections.Generic.List[string]]::new()
foreach ($tag in ((Get-Content -LiteralPath $defaultTagsFile -Raw) -split "[,\s]+")) {
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
foreach ($tag in $ExcludeTags) {
    $trimmed = $tag.Trim()
    if ($trimmed) {
        [void]$tags.Remove($trimmed)
    }
}
$tagString = [string]::Join(" ", $tags)
$ldflags = (Get-Content -LiteralPath $ldflagsFile -Raw).Trim()

Write-Host "Building linux/$Arch sing-box from $sourcePath"
Write-Host "Tags: $tagString"
Write-Host "Ldflags: $ldflags"
Write-Host "Output: $Output"

$dockerArgs = @(
    "run",
    "--rm",
    "-v", "${sourceDockerPath}:/src",
    "-v", "${outputParentDockerPath}:/out",
    "-w", "/src",
    "-e", "GOOS=linux",
    "-e", "GOARCH=$Arch",
    $GoImage,
    "go",
    "build",
    "-trimpath",
    "-buildvcs=false",
    "-tags", $tagString,
    "-ldflags", $ldflags,
    "-o", "/out/$outputName",
    "./cmd/sing-box"
)

& docker @dockerArgs
