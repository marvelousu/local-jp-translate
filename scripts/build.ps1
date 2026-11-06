param(
    [string]$Version = "dev",
    [string]$OutDir = "dist",
    [string]$Go = "go",
    [switch]$All
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$savedGOOS, $savedGOARCH, $savedCGO = $env:GOOS, $env:GOARCH, $env:CGO_ENABLED
Push-Location $root
try {
    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

    if ($All) {
        $targets = @(
            @{ OS = "windows"; Arch = "amd64" },
            @{ OS = "linux"; Arch = "amd64" },
            @{ OS = "darwin"; Arch = "arm64" }
        )
    } else {
        $targets = @(
            @{ OS = (& $Go env GOOS); Arch = (& $Go env GOARCH) }
        )
    }

    $env:CGO_ENABLED = "0"
    foreach ($target in $targets) {
        $env:GOOS = $target.OS
        $env:GOARCH = $target.Arch
        $ext = if ($target.OS -eq "windows") { ".exe" } else { "" }
        $out = Join-Path $OutDir ("jptranslate-" + $target.OS + "-" + $target.Arch + $ext)
        Write-Host "building $out"
        & $Go build -trimpath -buildvcs=false `
            -ldflags "-s -w -X main.version=$Version" `
            -o $out ./cmd/jptranslate
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $($target.OS)/$($target.Arch)"
        }
    }
} finally {
    $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $savedGOOS, $savedGOARCH, $savedCGO
    Pop-Location
}
