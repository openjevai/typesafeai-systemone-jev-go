# Changelog

All notable changes to this project are documented in this file. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-09-19

### Added

- `Client` with `SystemOne` and `ListModels`, configured through options or the
  `TYPESAFE_API_KEY`, `TYPESAFE_BASE_URL`, `TYPESAFE_DEFAULT_MODEL`, and
  `TYPESAFE_LOG_LEVEL` environment variables.
- Typed question primitives: `Noul`, `Choice`, `Score`, plus `RawQuestion` for
  forward compatibility.
- Typed answers with accessors: `NoulAnswer`, `ChoiceAnswer`, `ScoreAnswer`,
  and `UnknownAnswer` for unrecognized answer kinds.
- Async and batch wrappers: `SystemOneAsync`, `ListModelsAsync`, the generic
  `Future[T]`, and `StreamSystemOne` with bounded concurrency, completion-order
  or input-order delivery, and per-stream call options.
- `cmd/jev` command line: `models`, `noul`, `choice`, `score`, `ask`, and
  `batch` (NDJSON streaming of a JSON array of requests).
- Runnable cookbook examples: quickstart, triage, guardrails, rerank,
  extraction, and batch.
- Retries with jittered exponential backoff, `Retry-After` support, a retry
  budget, and per-call overrides.
- Typed errors (`APIError`, `AuthenticationError`, `RateLimitError`,
  `UnprocessableEntityError`, `InternalServerError`, `TimeoutError`,
  `ConnectionError`, `InvalidResponseError`) inspectable with `errors.As`.
- Tests against the vendored OpenAPI specification and opt-in end-to-end tests
  against the live TypeSafe API.

[Unreleased]: https://github.com/mheers/typesafeai-systemone-jev-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/mheers/typesafeai-systemone-jev-go/releases/tag/v0.1.0
