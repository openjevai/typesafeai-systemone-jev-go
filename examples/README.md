# Examples

Runnable programs that show the patterns from the
[TypeSafe cookbooks](https://docs.typesafe.ai/llms.txt), using this SDK.

Every example reads `TYPESAFE_API_KEY` from the environment:

```bash
export TYPESAFE_API_KEY=...
go run ./examples/<name>
```

| Example | Pattern | Shows |
| --- | --- | --- |
| [quickstart](quickstart) | First request | Noul, Choice, and Score in one call, and reading the answers |
| [triage](triage) | Composite scoring | Several Scores combined with weights, plus confidence-gated routing |
| [guardrails](guardrails) | LLM guardrails | One request per hazard, policy thresholds in code |
| [rerank](rerank) | Reranking | One Score question per candidate passage, ranked in code |
| [extraction](extraction) | Pre-parsed value extraction | Regex candidates, model selection, verbatim copy and normalization |
| [batch](batch) | Async and streaming | `SystemOneAsync` for one call, `StreamSystemOne` for a bounded pool |

The CLI (`go run ./cmd/jev`) covers the same request shapes from the shell;
run `jev help` for usage.
