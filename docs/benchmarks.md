# Generation benchmarking

Use `scripts/bench/generation_benchmark.py` to compare generation latency and throughput between the MLX backend and GGUF.

## Prerequisites

- Build the MLX CLI: `go build -o ollmlx .`
- Start the Ollama service for GGUF comparisons: install Ollama and run `ollama serve` in another terminal so that `ollama run` can connect to `localhost:11434`.
- Ensure the models you plan to test are reachable (the script will `pull` them if missing).

## Running locally

```bash
python3 scripts/bench/generation_benchmark.py \
  --mlx-model mlx-community/Llama-3.2-1B-Instruct-4bit \
  --gguf-model Llama-3.2-1B-Instruct \
  --prompt "Explain the trade-offs between throughput and latency." \
  --iterations 5 \
  --warmup 1 \
  --output benchmark-results.json
```

Key flags:

- `--skip-gguf` runs only the MLX benchmark (useful if Ollama is unavailable).
- `--iterations` controls how many measured runs to execute; every run is timed so average, p50, p95, and p99 latencies are reported.
- `--output` writes a JSON summary with tokens/sec plus tail latencies for each backend.

Results are printed to stdout and persisted in `benchmark-results.json`.

## CI workflow

Benchmarks are available as an **optional** CI workflow: `.github/workflows/benchmarks.yaml`.

To run them in GitHub Actions:

1. Open the **Actions** tab.
2. Select **generation-benchmarks** and click **Run workflow**.
3. Adjust inputs (prompt, iterations, models, or skip GGUF) as needed.

The workflow builds `ollmlx`, installs `ollama` when GGUF runs are enabled, executes the benchmark script, and uploads the JSON results as an artifact.
