# ollmlx Change Log

## Unreleased

### Added
- One-step installer `scripts/install_ollmlx.sh` to build the `ollmlx` binary and install MLX Python dependencies.
- Experimental `/finetune` endpoint in `mlx_backend/server.py` (calls into `mlx_lm` if a finetune entrypoint exists).
- MLX backend best-effort defaults to Metal GPU at startup.
- Verbose CLI (`ollmlx run --verbose`) now emits Apple Silicon/MLX tuning tips.
- MLX chat requests with tools now return 501 with a clear message (tool-calling not yet supported for MLX).

### Changed
- README: clarified CLI/API parity (same verbs/flags/envs/endpoints as Ollama), noted MLX pull progress uses stable digests, and highlighted current MLX/tool-calling status. Added quick-start pull/run example and simplified install instructions to use the installer.
- docs/MLX_ARCHITECTURE.md: documented current MLX runtime parity (runner startup, health/load sequence, streaming/non-streaming response mapping, stable digests for pulls).

### Testing
- `go test ./...` (linker still warns about duplicate `-lobjc`).
