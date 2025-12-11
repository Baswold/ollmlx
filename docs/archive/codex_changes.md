# codex_changes (this session)

## New functionality
- Added experimental MLX tool-calling path for chat: MLX chat requests now route through `chatMLXModel`, format prompts with tool hints, and parse `tool_calls` JSON from the final response (non-streaming when tools are present). Files: `server/routes_mlx.go`, `server/routes.go`.
- Added experimental MLX fine-tuning hook: `/finetune` endpoint in `mlx_backend/server.py` delegates to `mlx_lm` if available (otherwise 501); MLX backend prefers Metal by default and reports device in `/health`.
- Enhanced `ollmlx run --verbose` output with Apple Silicon/MLX tips in CLI (`cmd/cmd.go`).
- Added one-step installer `scripts/install_ollmlx.sh` to build `ollmlx` and install MLX Python deps.

## Behavior changes
- MLX chat with tools now executes (non-stream) instead of returning 501; tool calls are parsed from a `{"tool_calls": [...]}` JSON envelope.
- MLX generate/tool streaming remains unchanged; streaming is forced off when tools are present in the MLX path for now.
- MLX pulls use stable digests for progress bars; MLX tool-calling is documented as experimental and non-streaming.

## Documentation updates
- README: clarified MLX/Metal focus, experimental finetune, MLX tool-calling behavior (non-stream, JSON tool_calls parsing), CLI parity, installer usage, and quick-start pull/run instructions.
- docs/MLX_ARCHITECTURE.md: noted Metal default, finetune hook, MLX chat/tool behavior, and verbose tips.
- CHANGELOG_OLLMLX.md: logged installer, finetune, Metal default, verbose tips, and MLX tool-call handling.
- TODO_NEXT_AGENT.md: next steps point to full MLX tool-calling parity.

## Testing & build
- `go test ./...` (passes; longstanding duplicate `-lobjc` linker warnings remain).
- `go build ./...` (passes; same warnings).
- `python3 -m py_compile mlx_backend/server.py` (passes).

## Notes
- MLX tool-calling is experimental: enforced non-stream when tools are present; relies on model emitting `{"tool_calls": [...]}` JSON. Further work needed to fully align with Ollama’s streaming/tool parser behavior.
