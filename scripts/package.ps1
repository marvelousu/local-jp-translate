param(
    [string]$Version = "dev",
    [string]$OutDir = "dist",
    [string]$Go = "go"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $safeVersion = [regex]::Replace($Version, '[^A-Za-z0-9._-]', '-')
    & (Join-Path $PSScriptRoot "build.ps1") -Version $Version -OutDir $OutDir -Go $Go -All
    $archives = @()

    $stageRoot = Join-Path $OutDir ("stage-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Force -Path $stageRoot | Out-Null
    $resolvedOutput = (Resolve-Path -LiteralPath $OutDir).Path
    $resolvedStage = (Resolve-Path -LiteralPath $stageRoot).Path
    if ((Split-Path -Parent $resolvedStage) -ne $resolvedOutput -or (Split-Path -Leaf $resolvedStage) -notmatch '^stage-[0-9a-f]{32}$') {
        throw 'Refusing staging cleanup outside the intended output directory'
    }
    try {
        $targets = @(
            @{ OS = "windows"; Arch = "amd64" },
            @{ OS = "linux"; Arch = "amd64" },
            @{ OS = "darwin"; Arch = "arm64" }
        )
        foreach ($target in $targets) {
            $name = "local-jp-translate_${safeVersion}_$($target.OS)_$($target.Arch)"
            $bundle = Join-Path $stageRoot $name
            New-Item -ItemType Directory -Force -Path (Join-Path $bundle "docs") | Out-Null

            $ext = if ($target.OS -eq "windows") { ".exe" } else { "" }
            $binary = Join-Path $OutDir ("jptranslate-" + $target.OS + "-" + $target.Arch + $ext)
            Copy-Item $binary (Join-Path $bundle ("jptranslate" + $ext))
            Copy-Item README.md, README.en.md, LICENSE, THIRD_PARTY_NOTICES.md $bundle
            Copy-Item (Join-Path "docs" "API.md") (Join-Path $bundle "docs")
            if (Test-Path -LiteralPath "LICENSES") {
                Copy-Item -LiteralPath "LICENSES" -Destination $bundle -Recurse
            }
            if ($target.OS -eq "windows" -and (Test-Path -LiteralPath (Join-Path "scripts" "setup.ps1"))) {
                New-Item -ItemType Directory -Force -Path (Join-Path $bundle "scripts") | Out-Null
                Copy-Item (Join-Path "scripts" "setup.ps1") (Join-Path $bundle "scripts")
            }

            if ($target.OS -eq "windows") {
                $archive = Join-Path $OutDir ($name + ".zip")
                if (Test-Path $archive) { Remove-Item $archive -Force }
                Compress-Archive -Path $bundle -DestinationPath $archive
            } else {
                $archive = Join-Path $OutDir ($name + ".tar.gz")
                if (Test-Path $archive) { Remove-Item $archive -Force }
                if (-not (Get-Command python -ErrorAction SilentlyContinue)) { throw 'Python 3 is needed only to build Unix tarballs on Windows' }
                & python (Join-Path $PSScriptRoot 'package_tar.py') $bundle $archive
                if ($LASTEXITCODE -ne 0) { throw "tar packaging failed for $name" }
            }
            Write-Host "created $archive"
            $archives += $archive
        }

        $sumFile = Join-Path $resolvedOutput "SHA256SUMS.txt"
        $sumLines = foreach ($archive in $archives) {
            $hash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
            "$hash  $(Split-Path -Leaf $archive)"
        }
        $sumText = ($sumLines -join "`n") + "`n"
        [System.IO.File]::WriteAllText($sumFile, $sumText, (New-Object System.Text.UTF8Encoding($false)))
        Write-Host "created $sumFile"
    } finally {
        Remove-Item -LiteralPath $resolvedStage -Recurse -Force -ErrorAction SilentlyContinue
    }
} finally {
    Pop-Location
}
