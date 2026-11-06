# local-jp-translate

日本語 | [English](README.en.md)

PLaMo 3 Translate 0.8B をローカルの `llama-server` で動かし、日本語翻訳を HTTP API と CLI で使うツールです。翻訳処理はこの PC 内で完結します。ゲームやアプリの翻訳データを更新するツールから HTTP 経由で呼び出せます。配布物は Windows 10/11 (amd64)、Linux (amd64)、macOS (arm64) 向けです。CPU で動作し、速度は CPU と `llama-server` のビルドに依存します。

## Windows でのインストールと起動

[GitHub Releases](https://github.com/marvelousu/local-jp-translate/releases/latest) から Windows amd64 用の zip（`local-jp-translate_<version>_windows_amd64.zip`）をダウンロードして展開し、できたフォルダで PowerShell を開きます。

```powershell
.\scripts\setup.ps1 -AcknowledgeModelLicense
```

実行ポリシーで拒否される場合は、次のコマンドを使います。

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1 -AcknowledgeModelLicense
```

セットアップは llama.cpp の CPU 版（b12144）とモデル重み（約 0.57 GB）をダウンロードし、SHA256 を検証します。完了後、次のコマンドで起動します。

```powershell
.\jptranslate.exe serve `
  --llama-server .\runtime\llama-b12144-cpu\llama-server.exe `
  --model .\models\plamo-3-translate-0.8b-Q4_K_M.gguf
```

`http://127.0.0.1:8787` で API が起動します。このターミナルは開いたままにし、停止するときは Ctrl+C を押します。

配布 zip の `jptranslate.exe` を使うだけなら、Go も Python も不要です。

## 翻訳を試す

API を起動したまま、別の PowerShell から1文を送ります。

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

PLaMo 3 Translate 0.8B で実行した結果です。

```text
変更内容を保存してください。
```

CLI を使うと、訳文の本文だけが返ります。

```powershell
.\jptranslate.exe translate "Please save your changes." --target ja --style polite
```

```text
変更内容を保存してください。
```

用語集を指定して JSON で受け取るには、次のようにします。

```powershell
.\jptranslate.exe translate "Please open the project dashboard." `
  --target ja --style polite `
  --glossary "dashboard => 管理画面" `
  --json
```

`style` は `polite`、`plain`、`default` のいずれかです。`polite` と `plain` は `target=ja` のときだけ有効です。`translate` は `--url` で接続先を変更でき（既定は `http://127.0.0.1:8787`）、テキストを省略すると stdin から読みます。

## Linux / macOS でのインストールと起動

[GitHub Releases](https://github.com/marvelousu/local-jp-translate/releases/latest) から `local-jp-translate_<version>_linux_amd64.tar.gz` または `local-jp-translate_<version>_darwin_arm64.tar.gz` を展開し、できたフォルダに移動します。`scripts/setup.ps1` は Windows 専用なので、Linux と macOS では使いません。

`llama-server` は別に用意します。[llama.cpp のリリース](https://github.com/ggml-org/llama.cpp/releases)から取得するか、ソースからビルドしてパスを確認します。モデルは次のコマンドで取得できます。

```bash
./jptranslate model download --dir models --acknowledge-license

./jptranslate serve \
  --llama-server /path/to/llama-server \
  --model ./models/plamo-3-translate-0.8b-Q4_K_M.gguf
```

API は `http://127.0.0.1:8787` で起動します。`curl` で送る場合:

```bash
curl -s http://127.0.0.1:8787/translate \
  -H 'Content-Type: application/json' \
  -d '{"text":"Please save your changes.","source":"en","target":"ja","style":"polite"}'
```

配布された実行ファイルの利用に Go も Python も不要です。ソースからビルドする場合だけ Go 1.23 以上が必要です。

## ソースからビルドする

Go 1.23 以上が必要です。

```powershell
go build -o jptranslate.exe ./cmd/jptranslate
```

Linux / macOS では `go build -o jptranslate ./cmd/jptranslate` とします。

Windows では、ビルドした `jptranslate.exe` を置いたフォルダで `.\scripts\setup.ps1 -AcknowledgeModelLicense` を実行すると、`runtime\llama-b12144-cpu\llama-server.exe` とモデルを取得できます。すでに `llama-server` とモデルがある場合は、そのパスを `serve` に指定します。

```powershell
.\jptranslate.exe serve `
  --llama-server .\runtime\llama-b12144-cpu\llama-server.exe `
  --model .\models\plamo-3-translate-0.8b-Q4_K_M.gguf
```

## 起動方法

| 方法 | 指定 | 動作 |
| --- | --- | --- |
| 管理 | `--llama-server` と `--model` | このプログラムが `llama-server` を起動・監視・停止します。既定では `127.0.0.1:8788`、コンテキスト 16384 を使います。 |
| 外部 | `--backend http://127.0.0.1:8000` | 自分で起動した `llama-server` に接続します。GPU 設定や起動オプションを自分で管理する場合に使います。 |

- `--backend` と `--llama-server` は同時に指定できません。
- `--llama-server` を使うときは `--model` が必須です。
- `--backend` の既定値は `http://127.0.0.1:8000` です。`serve` だけを実行すると外部バックエンドに接続します。
- `--backend` と `--addr` は loopback アドレスだけを受け付けます。
- 起動後、`GET /ready` でバックエンドの準備状態を確認できます。

外部の `llama-server` を使う例:

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

`llama-server` が見つからない場合は、実行ファイルのあるフォルダで起動するか PATH に追加してください。その他の起動オプションは [docs/API.md](docs/API.md#サーバー起動オプション) にまとめています。

## API

ベース URL は `http://127.0.0.1:8787` です。リクエストとレスポンスは UTF-8 JSON で、POST には `Content-Type: application/json` が必要です。

- `POST /translate` — 翻訳
- `POST /v1/chat/completions` — 翻訳専用の互換 API。一般チャット、ストリーミング、system プロンプト、ツール呼び出しには対応しません。
- `GET /health` — プロセスの稼働状況とキュー統計
- `GET /ready` — `llama-server` の準備状態。未ロードなら `503`
- `GET /languages` — 対応言語の一覧

リクエストとレスポンスの詳細、エラーコード、起動オプションは [docs/API.md](docs/API.md) を参照してください。

## モデルとライセンス

Preferred Networks の PLaMo 3 Translate を利用しています（Built with PLaMo）。コードは [MIT](LICENSE)、モデル重みは PLaMo Community License で提供されます。既定モデルは次のファイルです。

| 項目 | 値 |
| --- | --- |
| リポジトリ | `pfnet/plamo-3-translate-0.8b-GGUF` |
| ファイル | `plamo-3-translate-0.8b-Q4_K_M.gguf` |
| リビジョン | `dbeb0856cb0b004b484f190bb71dd65876297013` |
| サイズ | 573,123,200 bytes |
| SHA256 | `923b2f28a9856de1b8b3b610a7e59ded0b14d007466fd5dd526e6a9ecaadafdd` |
| 重みのライセンス | PLaMo Community License |

```powershell
# モデル情報を表示
.\jptranslate.exe model info

# ダウンロード済みモデルを検証
.\jptranslate.exe model verify --dir models
```

個人・趣味・学術などの非商用利用は無償で、登録は不要です。商用利用や再配布の条件、ライセンス本文へのリンクは [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) にまとめています。モデル重みは配布物に含めず、初回セットアップで取得します。

## 制限

- loopback 専用です。同じ OS ユーザーで動く別プロセスから隔離する認証機能はありません。
- 推論スロットは1つで、待機キューは既定で8件です。高スループット用途には向きません。
- 長文の自動分割には対応していません。
- ストリーミング、任意の system プロンプト、ツール呼び出しには対応しません。
- `llama-server` の `/v1/completions` と `/health` に依存します。
- 実モデルの推論は Windows amd64 の CPU で確認しています。GPU 対応の `llama-server` も、外部バックエンドとして指定できます。
- CORS は `--cors-origin` で明示したオリジンだけを許可します（既定は無効）。

## 開発

```powershell
gofmt -l .
go vet ./...
go test ./...
go build ./cmd/jptranslate
```

クロスビルドとパッケージ:

```powershell
.\scripts\build.ps1 -All
.\scripts\package.ps1
```

Linux / macOS の場合:

```bash
bash ./scripts/build.sh --all
bash ./scripts/package.sh
```

対象は `windows/amd64`、`linux/amd64`、`darwin/arm64` です。Windows で Linux / macOS 向け tar.gz を作るときだけ Python 3 を使います。配布した実行ファイルの利用に Python は不要です。

## ドキュメント

- [docs/API.md](docs/API.md) — API の仕様、エラーコード、起動オプション
- [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) — モデルと llama.cpp のライセンス情報
- `LICENSES/` — モデルライセンス本文
