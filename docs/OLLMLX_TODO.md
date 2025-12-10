# OLLMLX Development TODO

A comprehensive action plan for transforming this repository into **ollmlx**: an MLX-powered, Apple-Silicon-optimized drop-in replacement for Ollama with the same CLI and HTTP interfaces.

## Guiding Principles
- Preserve **100% CLI and API compatibility** with upstream Ollama (same commands, ports, and response schemas).
- Optimize specifically for **Apple Silicon** using MLX; GGUF/llama.cpp paths can be removed or stubbed where appropriate.
- Prefer **minimal surface-area changes** to the CLI/daemon layers; concentrate differences in the inference backend.
- Ensure **observability and test coverage** for parity with upstream behavior (functional, streaming, and load tests).

## Work Breakdown Structure

### 1) Repository & Build System Preparation
- [ ] Audit current repo layout vs upstream Ollama to confirm divergence points.
- [ ] Decide fork strategy: long-lived fork vs. shallow wrapper around upstream modules.
- [ ] Introduce an `mlx` build tag / module boundary to keep MLX-specific code isolated.
- [ ] Add Apple Silicon CI lane (macOS ARM64) with MLX prerequisites cached.
- [ ] Document build requirements (Go toolchain version, Python/MLX deps, Homebrew packages).

### 2) Inference Backend Swap (GGUF → MLX)
- [ ] Map all inference entry points (likely under `llm/`, `server/`, `runner/`).
- [ ] Remove or stub llama.cpp/GGUF bindings; preserve interfaces used by CLI/server.
- [ ] Implement MLX backend wrapper (Python service or Go bindings) exposing:
  - [ ] Model load/init
  - [ ] Tokenization/embedding hooks
  - [ ] Text generation with streaming
  - [ ] Token/usage accounting
- [ ] Define IPC/transport between Go daemon and MLX service (gRPC/HTTP/unix socket/stdio).
- [ ] Implement backpressure and cancellation semantics compatible with Ollama streaming.

### 3) Model Format & Lifecycle
- [ ] Establish MLX model discovery from Hugging Face (search + metadata cache).
- [ ] Implement `pull` flow: download MLX checkpoint, verify hash, place in Ollama-compatible cache layout.
- [ ] Provide optional conversion path when MLX artifacts are missing (clear errors otherwise).
- [ ] Track model metadata (parameters, quantization, tokenizer info) for `/api/show`.
- [ ] Implement `list`, `delete`, and `create` commands against MLX artifacts.
- [ ] Define local caching strategy and eviction policy for unified memory constraints.

### 4) HTTP & CLI Parity
- [ ] Ensure `/api/generate`, `/api/chat`, `/api/pull`, `/api/tags`, `/api/delete`, `/api/show` mirror upstream schemas exactly.
- [ ] Validate streaming response framing and chunk timing against Ollama clients.
- [ ] Maintain CLI ergonomics (`run`, `pull`, `list`, `create`, `serve`) with identical flags and help text.
- [ ] Add compatibility tests against known Ollama client libraries and IDE extensions.

### 5) Performance & Memory
- [ ] Benchmark MLX vs. GGUF on M1/M2/M3 devices for representative model sizes.
- [ ] Tune quantization/precision defaults for MLX (e.g., mixed precision, GPU/CPU offload heuristics).
- [ ] Expose telemetry for token throughput, memory usage, and cache hits.
- [ ] Provide guidance for model-specific optimal settings (context length, batch size, KV cache behavior).

### 6) Error Handling & Reliability
- [ ] Normalize OOM and allocation errors to Ollama-equivalent error codes/messages.
- [ ] Add retries/backoff for model downloads and MLX service restarts.
- [ ] Ensure cancellation propagates across Go daemon and MLX subprocess.
- [ ] Add health checks for the MLX backend (liveness/readiness) surfaced via server diagnostics.

### 7) Testing & Verification
- [ ] Unit tests for request/response marshaling and backend adapters.
- [ ] Golden tests comparing Ollama vs. OLLMLX responses for canonical prompts.
- [ ] Streaming tests that assert chunk boundaries and timing tolerances.
- [ ] Load tests on macOS ARM64 hardware; collect latency/throughput distributions.
- [ ] Integration tests for model lifecycle (`pull → run → delete`).

### 8) Tooling & Developer Experience
- [ ] Provide dev scripts for macOS setup (Python venv, MLX install, Homebrew deps).
- [ ] Add make targets for running the MLX backend and Go server together.
- [ ] Offer logging presets for debugging IPC traffic and token-level traces.
- [ ] Ship example Modelfiles for popular MLX models (e.g., Llama 3, Mistral) with prompt templates.

### 9) Documentation & Communication
- [ ] Write a dedicated README section describing the MLX backend architecture and Apple-Silicon focus.
- [ ] Document limitations (MLX-only, Apple Silicon only, model availability caveats).
- [ ] Provide migration guide for existing Ollama users (drop-in replacement instructions).
- [ ] Publish performance results and troubleshooting guide.

### 10) Release & Maintenance
- [ ] Define versioning policy relative to upstream Ollama releases.
- [ ] Automate release packaging/notarization for macOS binaries.
- [ ] Establish issue triage labels (compatibility, performance, model support).
- [ ] Plan periodic syncs with upstream to ingest CLI/API changes while preserving MLX backend.

## Immediate Next Steps (Day 1–3)
- [ ] Trace inference call graph to identify GGUF touchpoints.
- [ ] Prototype MLX backend service that can stream tokens for a small model.
- [ ] Verify CLI/server can forward a simple prompt through the MLX service with matching JSON schema.
- [ ] Set up macOS ARM64 CI smoke test executing `/api/generate` against a tiny MLX model.

## Open Research Questions
- Optimal IPC choice (latency vs. simplicity) for Go ↔ MLX.
- Coverage of popular Ollama models in MLX format; prioritize a short-list with benchmarks.
- Quantization strategies that best exploit unified memory without regressions.

## Success Criteria
- All Ollama CLI commands and HTTP endpoints operate unchanged with MLX backend on Apple Silicon.
- Performance on M-series Macs is measurably better than GGUF/llama.cpp for comparable models.
- Clear documentation and automated tests protect compatibility for downstream tools and IDE integrations.
