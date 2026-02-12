# smartsh training dataset schema

This directory contains starter data for building/fine-tuning an instruction model that emits `smartsh` JSON output.

## File format

- Use JSON Lines (`.jsonl`)
- One record per line
- UTF-8 encoding

## Record schema

Each line must follow:

```json
{
  "instruction": "string",
  "input": "stringified JSON environment object",
  "output": "stringified JSON response object"
}
```

## `instruction`

- Natural-language user request
- Examples:
  - `run this project`
  - `build all go packages`
  - `start docker services`

## `input`

String containing environment JSON. Suggested fields:

```json
{
  "os": "darwin|windows|linux",
  "project_type": "node|go|python|dotnet|java|docker|c_cpp|rust|generic",
  "workspace_kind": "nx|angular|javascript_monorepo|single_project",
  "package_manager": "npm|pnpm|yarn|bun",
  "runtimes": {
    "go": true,
    "node": false
  },
  "detected_files": ["go.mod", "nx.json"]
}
```

## `output`

String containing strict target JSON:

```json
{
  "intent": "string",
  "command": "string",
  "confidence": 0.0,
  "risk": "low|medium|high"
}
```

Rules:

- No additional keys
- `confidence` must be in range `[0,1]`
- `risk` must be one of `low`, `medium`, `high`
- Keep command practical and minimal

## Command safety guidance

Include high-risk intent examples in data, but keep safety enforcement deterministic in `smartsh` security layer.
The model suggests; validator blocks dangerous commands.

## Validation

Use:

```bash
go run ./scripts/validate-training-data --file ./training/smartsh_train.jsonl
```
