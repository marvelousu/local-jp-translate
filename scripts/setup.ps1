param([switch]$AcknowledgeModelLicense)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$taskRoot = Split-Path -Parent $PSScriptRoot
$taskCLI = Join-Path $taskRoot 'jptranslate.exe'
if (-not (Test-Path -LiteralPath $taskCLI)) { $taskCLI = Join-Path $taskRoot 'bin\jptranslate.exe' }
if (-not (Test-Path -LiteralPath $taskCLI)) { throw 'jptranslate.exe was not found. Use a release package or build bin/jptranslate.exe first.' }
if (-not $AcknowledgeModelLicense) {
    Write-Host 'PLaMo model: personal/noncommercial use is permitted under its community license.'
    Write-Host 'Commercial use requires registration and is subject to revenue/affiliate conditions.'
    Write-Host 'https://huggingface.co/pfnet/plamo-3-translate-0.8b-GGUF/blob/dbeb0856cb0b004b484f190bb71dd65876297013/LICENSE/ja'
    throw 'Read the bundled model license, then rerun with -AcknowledgeModelLicense.'
}
$taskRuntime = Join-Path $taskRoot 'runtime\llama-b12144-cpu'
$taskServer = Join-Path $taskRuntime 'llama-server.exe'
if (-not (Test-Path -LiteralPath $taskServer)) {
    $taskRuntimeParent = Split-Path -Parent $taskRuntime
    New-Item -ItemType Directory -Path $taskRuntimeParent -Force | Out-Null
    $taskArchive = Join-Path $taskRuntimeParent ('llama-' + [guid]::NewGuid().ToString('N') + '.zip')
    $taskStage = Join-Path $taskRuntimeParent ('stage-' + [guid]::NewGuid().ToString('N'))
    try {
        Invoke-WebRequest -UseBasicParsing -Uri 'https://github.com/ggml-org/llama.cpp/releases/download/b12144/llama-b12144-bin-win-cpu-x64.zip' -OutFile $taskArchive
        $taskDigest = (Get-FileHash -LiteralPath $taskArchive -Algorithm SHA256).Hash
        if ($taskDigest -ne '569990fcbacbe305aef41490f71df70088474a59d514ae3b1ee65c130ee74555') { throw 'llama.cpp archive checksum mismatch; not installing.' }
        Expand-Archive -LiteralPath $taskArchive -DestinationPath $taskStage
        if (-not (Test-Path -LiteralPath (Join-Path $taskStage 'llama-server.exe'))) { throw 'llama-server.exe missing from verified archive.' }
        if (Test-Path -LiteralPath $taskRuntime) { throw 'Runtime destination already exists; left unchanged.' }
        Move-Item -LiteralPath $taskStage -Destination $taskRuntime
    } finally {
        if (Test-Path -LiteralPath $taskArchive) { Remove-Item -LiteralPath $taskArchive }
        # Failed staging directories are retained for diagnosis; never recursively
        # delete a computed user-selected destination during setup.
    }
}
& $taskCLI model download --dir (Join-Path $taskRoot 'models') --acknowledge-license
if ($LASTEXITCODE -ne 0) { throw 'Model installation failed; no server was started.' }
Write-Host 'Setup complete. Start the local translation API with:'
Write-Host ('& "' + $taskCLI + '" serve --llama-server "' + $taskServer + '" --model "' + (Join-Path $taskRoot 'models\plamo-3-translate-0.8b-Q4_K_M.gguf') + '"')
