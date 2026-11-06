# local-jp-translate

[日本語](README.md) | English

A tool for running PLaMo 3 Translate 0.8B on a local `llama-server` and using Japanese translation through an HTTP API and CLI. Translation runs entirely on this PC. Tools that update translation data for games or applications can call it over HTTP. Release builds target Windows 10/11 (amd64), Linux (amd64), and macOS (arm64). It runs on the CPU, and speed depends on the CPU and on how `llama-server` was built.

## Install and start on Windows

Download the Windows amd64 zip (`local-jp-translate_<version>_windows_amd64.zip`) from [GitHub Releases](https://github.com/marvelousu/local-jp-translate/releases/latest), extract it, and open PowerShell in the resulting folder.

```powershell
.\scripts\setup.ps1 -AcknowledgeModelLicense
```

If the execution policy blocks the script, use the following command.

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1 -AcknowledgeModelLicense
```

Setup downloads the llama.cpp CPU build (b12144) and the model weights (about 0.57 GB), and verifies the SHA256 checksums. After it completes, start the server with the following command.

```powershell
.\jptranslate.exe serve `
  --llama-server .\runtime\llama-b12144-cpu\llama-server.exe `
  --model .\models\plamo-3-translate-0.8b-Q4_K_M.gguf
```

The API starts at `http://127.0.0.1:8787`. Keep this terminal open and press Ctrl+C to stop it.

Using only the `jptranslate.exe` from the release zip requires neither Go nor Python.

## Try a translation

With the API running, send one sentence from another PowerShell window.

```powershell
$body = @{
    text   = "Please save your changes."
    source = "en"
    target = "ja"
    style  = "polite"
} | ConvertTo-Json

$response = Invoke-RestMethod -Method Post `
    -Uri http://127.0.0.1:8787/translate `
    -ContentType 'application/json' `
    -Body ([Text.Encoding]::UTF8.GetBytes($body))

$response.translated_text
```

Result from running PLaMo 3 Translate 0.8B.

```text
変更内容を保存してください。
```

The CLI returns only the translated text.

```powershell
.\jptranslate.exe translate "Please save your changes." --target ja --style polite
```

```text
変更内容を保存してください。
```

To pass a glossary and receive JSON, use the following.

```powershell
.\jptranslate.exe translate "Please open the project dashboard." `
  --target ja --style polite `
  --glossary "dashboard => 管理画面" `
  --json
```

`style` is one of `polite`, `plain`, or `default`. `polite` and `plain` are valid only when `target=ja`. `translate` accepts `--url` to change the server (the default is `http://127.0.0.1:8787`), and reads from stdin when text is omitted.

## Install and start on Linux / macOS

Download `local-jp-translate_<version>_linux_amd64.tar.gz` or `local-jp-translate_<version>_darwin_arm64.tar.gz` from [GitHub Releases](https://github.com/marvelousu/local-jp-translate/releases/latest), extract it, and change into the resulting folder. `scripts/setup.ps1` is Windows-only, so it is not used on Linux or macOS.

`llama-server` must be provided separately. Get it from the [llama.cpp releases](https://github.com/ggml-org/llama.cpp/releases) or build it from source, and note its path. You can download the model with the following command.

```bash
./jptranslate model download --dir models --acknowledge-license

./jptranslate serve \
  --llama-server /path/to/llama-server \
  --model ./models/plamo-3-translate-0.8b-Q4_K_M.gguf
```

The API starts at `http://127.0.0.1:8787`. To send a request with `curl`:

```bash
curl -s http://127.0.0.1:8787/translate \
  -H 'Content-Type: application/json' \
  -d '{"text":"Please save your changes.","source":"en","target":"ja","style":"polite"}'
```

Neither Go nor Python is required to use the released executable. Go 1.23 or later is required only when building from source.

## Build from source

Go 1.23 or later is required.

```powershell
go build -o jptranslate.exe ./cmd/jptranslate
```

On Linux / macOS, use `go build -o jptranslate ./cmd/jptranslate`.

On Windows, running `.\scripts\setup.ps1 -AcknowledgeModelLicense` in the folder containing the built `jptranslate.exe` downloads `runtime\llama-b12144-cpu\llama-server.exe` and the model. If you already have `llama-server` and the model, pass their paths to `serve`.

```powershell
.\jptranslate.exe serve `
  --llama-server .\runtime\llama-b12144-cpu\llama-server.exe `
  --model .\models\plamo-3-translate-0.8b-Q4_K_M.gguf
```

## Startup modes

| Mode | Options | Behavior |
| --- | --- | --- |
| Managed | `--llama-server` and `--model` | This program starts, monitors, and stops `llama-server`. By default it uses `127.0.0.1:8788` and a context size of 16384. |
| External | `--backend http://127.0.0.1:8000` | Connects to a `llama-server` you started yourself. Use this when you want to manage GPU settings and startup options yourself. |

- `--backend` and `--llama-server` cannot be used together.
- `--model` is required when using `--llama-server`.
- The default value of `--backend` is `http://127.0.0.1:8000`. Running `serve` alone connects to an external backend.
- `--backend` and `--addr` accept loopback addresses only.
- After startup, `GET /ready` reports whether the backend is ready.

Example using an external `llama-server`:

```powershell
llama-server `
  -m .\models\plamo-3-translate-0.8b-Q4_K_M.gguf `
  -c 16384 `
  --host 127.0.0.1 `
  --port 8000 `
  --alias plamo-3-translate `
  --jinja `
  --no-prefill-assistant

.\jptranslate.exe serve --backend http://127.0.0.1:8000
```

If `llama-server` is not found, start it from the folder containing the executable or add it to PATH. Other startup options are listed in [docs/API.md](docs/API.md#サーバー起動オプション) (Japanese).

## API

The base URL is `http://127.0.0.1:8787`. Request and response bodies are UTF-8 JSON, and POST requests require `Content-Type: application/json`.

- `POST /translate` — translation
- `POST /v1/chat/completions` — translation-only compatible API. General chat, streaming, system prompts, and tool calls are not supported.
- `GET /health` — process liveness and queue statistics
- `GET /ready` — readiness of `llama-server`. Returns `503` if it is not loaded.
- `GET /languages` — list of supported languages

See [docs/API.md](docs/API.md) (Japanese) for request and response details, error codes, and startup options.

## Model and license

This project uses Preferred Networks' PLaMo 3 Translate (Built with PLaMo). The code is [MIT](LICENSE), and the model weights are provided under the [PLaMo Community License](LICENSES/PLAMO-en.txt). The default model is the following file.

| Item | Value |
| --- | --- |
| Repository | `pfnet/plamo-3-translate-0.8b-GGUF` |
| File | `plamo-3-translate-0.8b-Q4_K_M.gguf` |
| Revision | `dbeb0856cb0b004b484f190bb71dd65876297013` |
| Size | 573,123,200 bytes |
| SHA256 | `923b2f28a9856de1b8b3b610a7e59ded0b14d007466fd5dd526e6a9ecaadafdd` |
| Weights license | PLaMo Community License |

```powershell
# Show model information
.\jptranslate.exe model info

# Verify a downloaded model
.\jptranslate.exe model verify --dir models
```

Non-commercial use, such as personal, hobby, or academic use, is free and requires no registration. Commercial use and redistribution conditions, and links to the license texts, are summarized in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) (Japanese). The model weights are not included in the release and are downloaded during first-time setup.

## Limitations

- The server is loopback-only. There is no authentication that isolates it from other processes running under the same OS user.
- There is one inference slot, and the waiting queue holds 8 requests by default. It is not intended for high-throughput use.
- Automatic splitting of long texts is not supported.
- Streaming, arbitrary system prompts, and tool calls are not supported.
- It depends on `/v1/completions` and `/health` of `llama-server`.
- Inference with the actual model has been verified on Windows amd64 with a CPU. A GPU-enabled `llama-server` can also be specified as an external backend.
- CORS allows only origins explicitly specified with `--cors-origin` (disabled by default).

## Development

```powershell
gofmt -l .
go vet ./...
go test ./...
go build ./cmd/jptranslate
```

Cross-builds and packaging:

```powershell
.\scripts\build.ps1 -All
.\scripts\package.ps1
```

On Linux / macOS:

```bash
bash ./scripts/build.sh --all
bash ./scripts/package.sh
```

The targets are `windows/amd64`, `linux/amd64`, and `darwin/arm64`. Python 3 is used only when creating Linux / macOS tar.gz archives on Windows. Python is not required to use released executables.

## Documentation

- [docs/API.md](docs/API.md) — API specification, error codes, and startup options (Japanese)
- [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) — license information for the model and llama.cpp (Japanese)
- `LICENSES/` — full model license texts
