# local-jp-translate

PLaMo 3 Translate 0.8B をローカルの `llama-server` で動かし、日本語翻訳を HTTP API と CLI で使うツールです。翻訳はこの PC 内で完結し、クラウド推論やモデルの学習は行いません。

**Built with PLaMo** — Preferred Networks の PLaMo 3 Translate を利用しています。

- ソースコード: MIT
- モデル重み: PLaMo Community License（MIT ではありません）
- モデル重みは同梱しません。初回に公式リポジトリから取得します。

## できること

- `jptranslate serve` で `127.0.0.1:8787` に翻訳 API を起動
- 既存の loopback `llama-server` に接続するか、`llama-server` を子プロセスとして起動・停止
- `POST /translate` にテキスト、言語、文体、用語集、文脈を渡して翻訳
- `POST /v1/chat/completions`（翻訳専用・非ストリーミング）
- 単発翻訳 CLI `jptranslate translate`
- 公式 0.8B Q4_K_M の取得と SHA256 検証

ゲームやアプリの翻訳データを更新するツールから、HTTP 経由で呼び出せます。

## 必要環境

- Windows 10/11 (amd64)、Linux (amd64)、macOS (arm64) のいずれか
- [llama.cpp](https://github.com/ggml-org/llama.cpp) の `llama-server`
- 公式 GGUF `plamo-3-translate-0.8b-Q4_K_M.gguf`（約 0.57 GB）
- ソースからビルドする場合のみ Go 1.23 以上
- CPU で動作します。速度は CPU/GPU と `llama-server` のビルドに依存します。

Windows の配布 zip には `scripts/setup.ps1` が入っています。このスクリプトは固定バージョンの llama.cpp CPU ビルドとモデルを取得します。実行ファイルを使うだけなら Go も Python も不要です。

## Windows クイックスタート

### 配布 zip を使う場合

[GitHub Releases](https://github.com/marvelousu/local-jp-translate/releases/latest) から `windows_amd64.zip` をダウンロードして展開し、そのフォルダで PowerShell を開きます。Linux amd64 / macOS arm64 向けの配布物も同じページにあります。

```powershell
# llama.cpp とモデルを取得（モデルライセンスへの同意が必要）
.\scripts\setup.ps1 -AcknowledgeModelLicense
```

実行ポリシーでスクリプトが拒否される場合は、`powershell -ExecutionPolicy Bypass -File .\scripts\setup.ps1 -AcknowledgeModelLicense` を使ってください。

セットアップ完了時に起動コマンドが表示されます。例:

```powershell
.\jptranslate.exe serve `
  --llama-server .\runtime\llama-b12144-cpu\llama-server.exe `
  --model .\models\plamo-3-translate-0.8b-Q4_K_M.gguf
```

`http://127.0.0.1:8787` で API が起動します。

### ソースからビルドする場合

```powershell
go build -o jptranslate.exe ./cmd/jptranslate

# モデルを取得（ライセンスを確認してから）
.\jptranslate.exe model download --dir models --acknowledge-license

# llama-server をこのプログラムに管理させる
# --llama-server には実際の llama-server.exe のパスを指定する
.\jptranslate.exe serve `
  --llama-server .\llama-server.exe `
  --model .\models\plamo-3-translate-0.8b-Q4_K_M.gguf
```

`--acknowledge-license` が正式なフラグです。`--accept-license` も同じ意味です。同意フラグは商用登録を行いません。

## 翻訳を試す

別のターミナルから API を呼びます。

```powershell
$body = @{
    text   = "The meeting has been rescheduled to next Monday."
    source = "en"
    target = "ja"
    style  = "polite"
} | ConvertTo-Json

Invoke-RestMethod -Method Post `
    -Uri http://127.0.0.1:8787/translate `
    -ContentType 'application/json' `
    -Body ([Text.Encoding]::UTF8.GetBytes($body))
```

`translated_text`、`translatedText`、`model`、`duration_ms` を含む JSON が返ります。

CLI からも翻訳できます（API の起動中に実行）。

```powershell
.\jptranslate.exe translate "The meeting has been rescheduled to next Monday." --target ja --style polite
```

結果の本文だけが必要なときはこの形式が便利です。用語集や JSON 出力を使う場合:

```powershell
.\jptranslate.exe translate "Please open the project dashboard." `
  --target ja --style polite `
  --glossary "dashboard => 管理画面" `
  --json
```

`style` は `polite` / `plain` / `default` で、`polite` と `plain` は `target=ja` のときだけ有効です。`translate` は `--url` で接続先を変更できます（既定 `http://127.0.0.1:8787`）。テキストを省略すると stdin から読みます。

## バックエンドの選び方

| 方法 | 起動方法 | 特徴 |
| --- | --- | --- |
| 管理 | `--llama-server` と `--model` | このプログラムが `llama-server` を起動・監視・停止します。`llama-server` は既定で `127.0.0.1:8788`、コンテキスト 4096 を使います。 |
| 外部 | `--backend http://127.0.0.1:8000` | 自分で起動した `llama-server` に接続します。GPU 設定や起動オプションを自分で管理したい場合に使います。 |

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

`llama-server` が見つからない場合は、実行ファイルのあるフォルダで起動するか PATH に追加してください。

## API

主なエンドポイント:

- `POST /translate` — 翻訳
- `POST /v1/chat/completions` — 翻訳専用の互換 API。一般チャットやストリーミングには対応しません。
- `GET /health` — プロセスの稼働状況とキュー統計
- `GET /ready` — `llama-server` の準備状態。未ロードなら `503`
- `GET /languages` — 対応言語の一覧

リクエストとレスポンスの詳細、エラーコードは [docs/API.md](docs/API.md) を参照してください。エラーは次の形式です。

```json
{"error":{"code":"invalid_target_language","message":"unsupported target language: xx"}}
```

## モデルとライセンス

既定モデル（公式・固定マニフェスト）:

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

- 日本語ライセンス: <https://plamo.preferredai.jp/info/plamo-community-license-ja>
- 英語ライセンス: <https://plamo.preferredai.jp/info/plamo-community-license-en>
- 同梱ライセンス本文: `LICENSES/PLAMO-ja.txt`、`LICENSES/PLAMO-en.txt`
- 商用利用登録: <https://forms.gle/mTL8tBLrMYXKNZD56>

個人・趣味・学術などの非商用利用は無償です。商用利用は条件が異なります。登録が必要な場合は利用者自身で行ってください。このツールはライセンス同意や商用登録を自動化しません。

**ソースコードの MIT と、モデル重みの PLaMo Community License は別です。** MIT は Go コード、ドキュメント、スクリプトに適用されます。モデル重みには適用されません。詳細は [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) を参照してください。

## 制限

- loopback 専用です。同じ OS ユーザーで動く別プロセスから隔離する認証機能はありません。
- 推論スロットは 1 つです。待機キューは既定で 8 件です。高スループット用途には向きません。
- 翻訳の正しさ、対応外の言語ペア、長文の分割は保証しません。
- ストリーミング、任意の system プロンプト、ツール呼び出しには対応しません。
- `llama-server` の `/v1/completions` と `/health` に依存します。
- GPU 実行と、Windows amd64 以外の実機での推論は未検証です。
- CORS は `--cors-origin` で明示したオリジンだけを許可します（既定は無効）。

## ビルド・テスト・配布

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

- [docs/API.md](docs/API.md) — API の仕様とエラーコード
- [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) — モデルと llama.cpp のライセンス情報
- `LICENSES/` — モデルライセンス本文
