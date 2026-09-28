# OpenJEV Support

This fork adds optional support for [OpenJEV](https://openjev.sh), a free
community gateway to the same Jev model built by [TypeSafe](https://typesafe.ai).
TypeSafe remains the default; OpenJEV is opt-in and changes nothing for existing
users.

## What was added

- **`client.go`** — New constants `OpenJEVBaseURL` (`https://api.openjev.sh`),
  `ModelOpenJEV` (`openjev`), and environment variables `EnvOpenJEVAPIKey`
  (`OPENJEV_API_KEY`) and `EnvProvider` (`JEV_PROVIDER`). `NewClient` now
  resolves the provider before reading key/URL/model: explicit `JEV_PROVIDER`
  wins; otherwise TypeSafe if `TYPESAFE_API_KEY` is set; otherwise OpenJEV if
  only `OPENJEV_API_KEY` is set. The error message names the right key env var.
- **`doc.go`** — Package doc mentions the OpenJEV option.
- **`internal/cli/cli.go`** — `jev help` lists `OPENJEV_API_KEY` and
  `JEV_PROVIDER` in the configuration section.
- **`README.md`** — OpenJEV note after the intro, new env var table rows, and an
  OpenJEV section under Configuration with usage examples.

No TypeSafe code was removed, renamed, or re-defaulted. The retry policy
already covers 408, 429, and all 5xx (including 503), so no change was needed.

## Provider selection rule

1. `JEV_PROVIDER=openjev` — explicit choice, uses OpenJEV.
2. `TYPESAFE_API_KEY` set (and no `JEV_PROVIDER`) — TypeSafe, exactly as before.
3. Only `OPENJEV_API_KEY` set (no `TYPESAFE_API_KEY`, no `JEV_PROVIDER`) — OpenJEV.

`WithAPIKey`, `WithBaseURL`, and `WithModel` options always override the
provider selection, so programmatic callers can mix freely.

## How to configure

```bash
# Option A: auto-select OpenJEV (no TYPESAFE_API_KEY in environment)
export OPENJEV_API_KEY=...   # from https://openjev.sh/dashboard

# Option B: force OpenJEV even if TYPESAFE_API_KEY is also set
export JEV_PROVIDER=openjev
export OPENJEV_API_KEY=...
```

```go
// Auto-detects provider from available keys.
client, err := jev.NewClient()

// Or set everything programmatically.
client, err := jev.NewClient(
    jev.WithAPIKey(openjevKey),
    jev.WithBaseURL(jev.OpenJEVBaseURL),  // "https://api.openjev.sh"
    jev.WithModel(jev.ModelOpenJEV),      // "openjev"
)
```

The OpenJEV gateway uses the same request/response contract as TypeSafe
System One. Extra response fields (`usage.cost`, `id`, `provider`) are
ignored by the typed client.

## How it was verified

A live `POST https://api.openjev.sh/v1/systemone` request with model `openjev`,
state `ping`, and one noul question returned HTTP 200. A grep confirmed no
hardcoded `api.typesafe.ai` default was introduced (the only occurrences are
the unchanged TypeSafe default and test fixtures).

## Upstream

Original project: https://github.com/mheers/typesafeai-systemone-jev-go by
@marcelheers. MIT license, unchanged.
