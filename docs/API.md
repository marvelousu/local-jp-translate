# local-jp-translate API

Base URL: `http://127.0.0.1:8787` (loopback only).
All request and response bodies are UTF-8 JSON. POST requests should send
`Content-Type: application/json`.

## Errors

Every non-2xx response uses:

```json
{"error":{"code":"invalid_target_language","message":"unsupported target language: xx"}}
```

| HTTP | code | Meaning |
| ---: | --- | --- |
| 400 | `invalid_json` | Body is not one JSON object |
| 400 | `text_required` | Neither `text` nor `q` was provided |
| 400 | `ambiguous_text` | `text` and `q` were both provided with different values |
| 400 | `text_too_long`, `context_too_long` | Input exceeds the configured bound |
| 400 | `invalid_source_language`, `invalid_target_language` | Unsupported language code |
| 400 | `invalid_style`, `style_target_mismatch` | `style` is not polite/plain/default, or not Japanese target |
| 400 | `invalid_glossary` | Empty, oversized, or too many glossary entries |
| 400 | `reserved_token` | Input contains a reserved `<|plamo:` marker |
| 400 | `invalid_max_tokens` | `max_tokens` outside 1..2048 |
| 400 | `unsupported_messages`, `stream_unsupported`, `invalid_temperature` | Unsupported chat-completions shape |
| 404 | `not_found` | Unknown path |
| 405 | `method_not_allowed` | Wrong HTTP method |
| 413 | `payload_too_large` | Body exceeds the limit (default 1 MiB) |
| 429 | `overloaded` | Waiting queue is full; retry later |
| 502 | `backend_truncated` | Generation reached `max_tokens`; partial text is rejected |
| 502 | `backend_empty` | Model returned empty text |
| 502 | `backend_malformed` | Invalid upstream JSON or response shape |
| 502 | `backend_incomplete` | Missing or unexpected `finish_reason` |
| 502 | `unsafe_output` | Output contains a reserved PFML marker |
| 502 | `backend_error` | Upstream HTTP or transport failure |
| 503 | `not_ready` | llama-server is not ready |
| 504 | `request_timeout` | Timed out waiting for the inference slot |
| 504 | `upstream_timeout` | Translation timed out |

## POST /translate

### Request

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `text` | string | one of `text`/`q` | — | Source text |
| `q` | string | one of `text`/`q` | — | LibreTranslate-compatible alias |
| `source` | string | no | `auto` | `auto` or one of the 53 codes |
| `target` | string | no | `ja` | One of the 53 codes |
| `style` | string | no | `default` | `polite`, `plain`, `default`; only for `target=ja` |
| `glossary` | array | no | — | `[{"source":"...","target":"..."}]` |
| `context` | string | no | — | Reference context |
| `max_tokens` | integer | no | `512` | 1..2048 |

If both `text` and `q` are sent, they must be identical; otherwise the request
is rejected as ambiguous.

### Response 200

```json
{
  "translated_text": "会議は来週の月曜日に変更されました。",
  "translatedText": "会議は来週の月曜日に変更されました。",
  "model": "plamo-3-translate-0.8b-Q4_K_M",
  "duration_ms": 412
}
```

`translated_text` and `translatedText` are always identical. `duration_ms`
includes queue wait and inference.

### Example

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

Translation-only, nonstreaming compatibility endpoint. It is not a general
chat model and does not execute system prompts.

### Request

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

Constraints:

- `messages` must contain exactly one `user` message.
- `system`, `assistant`, tool calls, and multi-turn history are rejected.
- `stream` must be false/omitted.
- `temperature` must be 0 or omitted.
- `source_lang` is optional; `target_lang` defaults to `ja`.
- Custom `stop` values are rejected because translation uses fixed model stop markers.
  If `model` is supplied, it must match the configured model reported by `/ready`.
- `usage` is omitted because no tokenizer accounting is performed.

### Response 200

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

Returns immediately while the process is alive. Backend readiness is not
included here.

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

Checks llama-server's `/health` with a 2-second timeout.

```json
{"ready":true,"model":"plamo-3-translate-0.8b-Q4_K_M"}
```

`503 {"error":{"code":"not_ready",...}}` when the backend is loading, down, or
not configured.

## GET /languages

```json
{
  "languages": [{"code":"ja","name":"日本語"}, {"code":"en","name":"英語"}],
  "count": 53,
  "default_target": "ja",
  "source_auto": "auto"
}
```

The list follows the official PLaMo 3 Translate 0.8B GGUF model card:
`ja, en, zh-hans, zh-hant, ko, ar, az, be, bg, bn, ca, ceb, de, es, et, eu, fa,
fr, gu, he, hi, hr, hu, hy, id, it, jv, ka, kk, km, lt, lv, ml, mr, ms, my, ne,
nl, pa, pl, ru, si, sk, sl, sr, sw, ta, te, th, tl, ur, uz, vi`.

## Limits and concurrency

- Body: 1 MiB default (`--max-body-bytes`)
- `text`: 32 KiB; `context`: 32 KiB
- Glossary: 128 entries, 512 bytes per term
- Full PFML3 prompt: 256 KiB
- One active inference slot; bounded waiting queue (default 8)
- Per-request timeout: 180 seconds including queue wait
- Client disconnect cancels the upstream llama-server request
- Loopback listen/backend only, no authentication
- Optional CORS only for origins passed to `--cors-origin`

## Upstream behavior

The server calls `llama-server` at `POST /v1/completions` with a raw PFML3
prompt, `temperature: 0`, and
`stop: ["<|plamo:tag|>", "<|plamo:bos|>"]`. It does not prepend BOS.
A response is successful only when `finish_reason` is `stop`, the text is
non-empty, and it contains no `<|plamo:` marker. Partial completions are never
returned as successful translations.
