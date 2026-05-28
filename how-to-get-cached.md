# How to Get Cached Token Count from API Responses

Cache hit token count should come from the API response `usage` metadata. Do not estimate it locally from prompt text length.

## Field mapping

Different APIs expose the same concept with slightly different field names.

### OpenAI Responses API

Read cache hits from `usage.input_tokens_details.cached_tokens`:

```js
const cacheHitTokens = response.usage?.input_tokens_details?.cached_tokens ?? 0
```

Example:

```json
{
  "usage": {
    "input_tokens": 12000,
    "output_tokens": 800,
    "total_tokens": 12800,
    "input_tokens_details": {
      "cached_tokens": 9000
    }
  }
}
```

In this example, the cache hit count is `9000` input tokens.

### OpenAI Chat Completions API

Read cache hits from `usage.prompt_tokens_details.cached_tokens`:

```js
const cacheHitTokens = response.usage?.prompt_tokens_details?.cached_tokens ?? 0
```

Example:

```json
{
  "usage": {
    "prompt_tokens": 12000,
    "completion_tokens": 800,
    "total_tokens": 12800,
    "prompt_tokens_details": {
      "cached_tokens": 9000
    }
  }
}
```

### Aiden normalized usage

Aiden may normalize provider-specific usage fields into `input_token_details.cache_read`:

```js
const cacheHitTokens = response.usage?.input_token_details?.cache_read ?? 0
```

For example, Chat Completions `prompt_tokens_details.cached_tokens` can be normalized to `input_token_details.cache_read`.

## Recommended helper

Use a helper that supports all known shapes:

```js
function getCacheHitTokens(response) {
  const usage = response?.usage ?? {}

  return (
    usage.input_tokens_details?.cached_tokens ??
    usage.prompt_tokens_details?.cached_tokens ??
    usage.input_token_details?.cache_read ??
    0
  )
}
```

## Streaming responses

For streaming APIs, do not add every delta's usage blindly.

Use the final usage-bearing event/chunk when available:

- Responses API: read `response.usage` from the final `response.completed` event.
- Chat Completions API: enable usage in the stream if required, for example `stream_options.include_usage`, then read the final chunk's `usage`.

If the same request/message can report usage multiple times during streaming, accumulate by delta instead of adding the full value repeatedly:

```js
const creditedByRequest = new Map()

function recordUsage(requestId, usage) {
  const currentCached =
    usage.input_tokens_details?.cached_tokens ??
    usage.prompt_tokens_details?.cached_tokens ??
    usage.input_token_details?.cache_read ??
    0

  const credited = creditedByRequest.get(requestId) ?? 0
  const delta = Math.max(0, currentCached - credited)

  creditedByRequest.set(requestId, credited + delta)
  return delta
}
```

The session-level cached token count is the sum of these non-negative deltas.

## What the number means

`cached_tokens` / `cache_read` means the number of input tokens served from prompt cache for that API request.

It is different from:

- `input_tokens` / `prompt_tokens`: total input tokens, including cached and non-cached tokens.
- `output_tokens` / `completion_tokens`: generated output tokens.
- `total_tokens`: input plus output tokens.
- `cache_creation` / `cache_creation_input_tokens`: tokens newly written into cache, not cache hits.

## Summary

To get cache hit tokens:

1. Read the API `usage` object.
2. Prefer `input_tokens_details.cached_tokens` for Responses API.
3. Use `prompt_tokens_details.cached_tokens` for Chat Completions API.
4. Use `input_token_details.cache_read` if your runtime has normalized usage fields.
5. In streaming mode, use final usage or delta-based de-duplication.

