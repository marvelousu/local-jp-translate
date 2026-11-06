# local-jp-translate API

ベース URL: `http://127.0.0.1:8787`（loopback のみ）。
リクエストとレスポンスの本文は UTF-8 JSON です。POST には
`Content-Type: application/json` を付けます。

## エラー

2xx 以外のレスポンスはすべて次の形式です。

```json
{"error":{"code":"invalid_target_language","message":"unsupported target language: xx"}}
```

| HTTP | code | 意味 |
| ---: | --- | --- |
| 400 | `invalid_json` | 本文が1つの JSON オブジェクトではない |
| 400 | `text_required` | `text` も `q` も指定されていない |
| 400 | `ambiguous_text` | `text` と `q` が両方あり、値が異なる |
| 400 | `text_too_long`, `context_too_long` | 入力が設定した上限を超えている |
| 400 | `invalid_source_language`, `invalid_target_language` | 未対応の言語コード |
| 400 | `invalid_style`, `style_target_mismatch` | `style` が polite/plain/default 以外、または翻訳先が日本語ではない |
| 400 | `invalid_glossary` | 用語集が空、大きすぎる、または項目数が多すぎる |
| 400 | `reserved_token` | 入力に予約済みの PFML マーカーが含まれる |
| 400 | `invalid_max_tokens` | `max_tokens` が 1..2048 の範囲外 |
| 400 | `unsupported_messages`, `stream_unsupported`, `stop_unsupported`, `model_unsupported`, `invalid_temperature` | 未対応の chat-completions 形式 |
| 403 | `invalid_host`, `nonlocal_peer`, `origin_denied` | loopback 以外の接続、または許可していないブラウザオリジン |
| 404 | `not_found` | 不明なパス |
| 405 | `method_not_allowed` | HTTP メソッドが違う |
| 413 | `payload_too_large` | 本文が上限（既定 1 MiB）を超えている |
| 415 | `content_type` | POST の `Content-Type` が `application/json` ではない |
| 429 | `overloaded` | 待機キューが満杯。時間をおいて再試行する |
| 502 | `backend_truncated` | 生成が `max_tokens` に達した。途中までの本文は返さない |
| 502 | `backend_empty` | モデルが空のテキストを返した |
| 502 | `backend_malformed` | 上流の JSON またはレスポンス形式が不正 |
| 502 | `backend_incomplete` | `finish_reason` がない、または想定外 |
| 502 | `unsafe_output` | 出力に予約済みの PFML マーカーが含まれる |
| 502 | `backend_error` | 上流の HTTP または通信に失敗した |
| 503 | `not_ready` | llama-server の準備ができていない |
| 504 | `request_timeout` | 推論スロットの待機がタイムアウトした |
| 504 | `upstream_timeout` | 翻訳がタイムアウトした |

`reserved_token` は、入力に `<|plamo:` で始まる予約済みマーカーが含まれる場合に返ります。

## POST /translate

### リクエスト

| フィールド | 型 | 必須 | 既定値 | 説明 |
| --- | --- | --- | --- | --- |
| `text` | string | `text` か `q` のどちらか | — | 原文 |
| `q` | string | `text` か `q` のどちらか | — | LibreTranslate 互換の別名 |
| `source` | string | いいえ | `auto` | `auto` または 53 言語のコード |
| `target` | string | いいえ | `ja` | 53 言語のコード |
| `style` | string | いいえ | `default` | `polite`、`plain`、`default`。`target=ja` のときだけ有効 |
| `glossary` | array | いいえ | — | `[{"source":"...","target":"..."}]` |
| `context` | string | いいえ | — | 参考にする文脈 |
| `max_tokens` | integer | いいえ | `512` | 1..2048 |

`text` と `q` を両方送る場合は同じ値にしてください。異なる場合は曖昧なリクエストとして拒否されます。

### レスポンス 200

```json
{
  "translated_text": "会議は来週の月曜日に変更されました。",
  "translatedText": "会議は来週の月曜日に変更されました。",
  "model": "plamo-3-translate-0.8b-Q4_K_M",
  "duration_ms": 412
}
```

`translated_text` と `translatedText` は常に同じ値です。`duration_ms` は待機時間と推論時間を含みます。

### 例

```bash
curl -s http://127.0.0.1:8787/translate \
  -H 'Content-Type: application/json' \
  -d '{
    "text": "Please open the project dashboard.",
    "source": "en",
    "target": "ja",
    "style": "polite",
    "glossary": [{"source": "dashboard", "target": "管理画面"}],
    "context": "Help page of a project management tool.",
    "max_tokens": 512
  }'
```

## POST /v1/chat/completions

翻訳専用の非ストリーミング互換エンドポイントです。一般のチャットモデルではなく、system プロンプトも実行しません。

### リクエスト

```json
{
  "model": "plamo-3-translate",
  "messages": [{"role": "user", "content": "Hello."}],
  "source_lang": "en",
  "target_lang": "ja",
  "temperature": 0,
  "max_tokens": 512
}
```

制約:

- `messages` には `user` メッセージをちょうど1つ含めます。
- `system`、`assistant`、ツール呼び出し、複数ターンの履歴は拒否されます。
- `stream` は false または省略にします。
- `temperature` は 0 または省略にします。
- `source_lang` は任意です。`target_lang` の既定値は `ja` です。
- 翻訳は固定の stop マーカーを使うため、独自の `stop` は拒否されます。
- `model` を指定する場合は、`/ready` が返す設定済みモデル名と一致させます。
- トークン数の集計を行わないため、`usage` は返しません。

### レスポンス 200

```json
{
  "id": "chatcmpl-1760000000-1",
  "object": "chat.completion",
  "created": 1760000000,
  "model": "plamo-3-translate-0.8b-Q4_K_M",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "こんにちは。"},
      "finish_reason": "stop"
    }
  ]
}
```

## GET /health

プロセスが生きている間はすぐに返ります。バックエンドの準備状態は含みません。

```json
{
  "status": "ok",
  "service": "local-jp-translate",
  "version": "dev",
  "model": "plamo-3-translate-0.8b-Q4_K_M",
  "uptime_seconds": 42,
  "queue": {"inflight": 0, "waiting": 0}
}
```

## GET /ready

llama-server の `/health` を2秒のタイムアウトで確認します。

```json
{"ready":true,"model":"plamo-3-translate-0.8b-Q4_K_M"}
```

バックエンドが読み込み中、停止中、または未設定の場合は
`503 {"error":{"code":"not_ready",...}}` を返します。

## GET /languages

```json
{
  "languages": [{"code":"ja","name":"日本語"}, {"code":"en","name":"英語"}],
  "count": 53,
  "default_target": "ja",
  "source_auto": "auto"
}
```

一覧は公式の PLaMo 3 Translate 0.8B GGUF モデルカードに従います。
`ja, en, zh-hans, zh-hant, ko, ar, az, be, bg, bn, ca, ceb, de, es, et, eu, fa,
fr, gu, he, hi, hr, hu, hy, id, it, jv, ka, kk, km, lt, lv, ml, mr, ms, my, ne,
nl, pa, pl, ru, si, sk, sl, sr, sw, ta, te, th, tl, ur, uz, vi`.

## 制限と同時実行

- 本文: 既定 1 MiB（`--max-body-bytes`）
- `text`: 32 KiB、`context`: 32 KiB
- 用語集: 128件、1項目 512 bytes
- PFML3 プロンプト全体: 256 KiB
- 推論スロットは1つ。待機キューは上限あり（既定 8）
- 1リクエストのタイムアウト: 180秒（待機時間を含む）
- クライアントが切断すると、上流の llama-server リクエストもキャンセルされる
- 待ち受けとバックエンドは loopback のみ。認証はない
- CORS は `--cors-origin` に渡したオリジンだけを許可する（既定は無効）
- loopback 以外の Host ヘッダーや接続元、許可していない Origin は 403 で拒否する

## サーバー起動オプション

`jptranslate serve` の主なフラグです。

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--addr` | `127.0.0.1:8787` | 待ち受けアドレス。loopback のみ |
| `--backend` | `http://127.0.0.1:8000` | 既存の `llama-server` の URL。loopback のみ。`--llama-server` とは同時に指定できない |
| `--llama-server` | なし | このプログラムが起動する `llama-server` のパス |
| `--model` | なし | GGUF モデルのパス。`--llama-server` を指定したときは必須で、単独では指定できない |
| `--llama-host` | `127.0.0.1` | 管理する `llama-server` の待ち受けホスト。loopback のみ |
| `--llama-port` | `8788` | 管理する `llama-server` のポート |
| `--llama-context` | `4096` | 管理する `llama-server` のコンテキスト長（256..32768） |
| `--llama-alias` | `plamo-3-translate` | 管理する `llama-server` のモデル別名 |
| `--llama-arg` | なし | `llama-server` に渡す追加引数。繰り返し指定でき、1フラグにつき1引数 |
| `--startup-timeout` | `180s` | 管理する `llama-server` の起動待ち時間 |
| `--request-timeout` | `180s` | 翻訳1件のタイムアウト。待機時間を含む |
| `--queue-size` | `8` | 推論中の1件を除く待機キューの上限。0 以上 |
| `--model-name` | モデルファイル名 | レスポンスに載せるモデル名。`--model` がない場合は `plamo-3-translate-0.8b-Q4_K_M` |
| `--max-body-bytes` | `1048576` | リクエスト本文の上限 |
| `--cors-origin` | なし | 許可するブラウザオリジン。完全一致で繰り返し指定。`--allow-origin` も同じ |

## CLI オプション

`jptranslate translate` の主なフラグです。

| フラグ | 既定値 | 説明 |
| --- | --- | --- |
| `--url` | `http://127.0.0.1:8787` | 接続先のサーバー |
| `--text` | なし | 翻訳するテキスト。位置引数や stdin とは併用しない |
| `--source` | `auto` | 原文の言語コードまたは `auto` |
| `--target` | `ja` | 訳文の言語コード |
| `--style` | `default` | `polite`、`plain`、`default`。`polite` と `plain` は `target=ja` のときだけ有効 |
| `--context` | なし | 参考にする文脈 |
| `--max-tokens` | `0` | 生成上限。0 はサーバー既定 |
| `--timeout` | `180s` | リクエストのタイムアウト |
| `--json` | なし | JSON 全体を表示する |
| `--glossary` | なし | `source => target` 形式。繰り返し指定できる |

テキストは位置引数、`--text`、stdin のいずれかで渡します。

## 上流の動作

サーバーは `llama-server` の `POST /v1/completions` を、生の PFML3 プロンプト、
`temperature: 0`、`stop: ["<|plamo:tag|>", "<|plamo:bos|>"]` で呼びます。BOS は先頭に付けません。
`finish_reason` が `stop`、本文が空でない、`<|plamo:` マーカーを含まない場合だけ成功とします。
途中までの補完は成功した翻訳として返しません。
