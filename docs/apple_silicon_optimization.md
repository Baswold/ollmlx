# Apple Silicon Optimization Guide

This guide collects practical steps to squeeze more performance from ollmlx on Apple Silicon Macs.

## Keep the MLX toolchain current
- Install the newest stable macOS and Xcode Command Line Tools so Metal kernels and the MLX runtime stay current.
- Update the `mlx` Python package regularly (`python -m pip install -U mlx`) to pick up fused kernels and Metal optimizations.

## Prefer MLX-native model formats
- Use MLX variants from `mlx-community` when available. They load directly into MLX without format conversion overhead.
- Favor 4-bit MLX models for speed and memory efficiency; they typically keep small and medium models within unified-memory limits while still delivering strong quality.

## Confirm the GPU path is active
- MLX uses Metal automatically on Apple Silicon. During a run, open **Activity Monitor → Window → GPU History** to verify GPU graphs are active.
- When evaluating throughput, keep the Mac on the power adapter and select **High Power Mode** (where available) to avoid thermal throttling.

## Optimize context and batching for Apple Silicon
- Start with conservative context sizes (e.g., 2k–4k tokens) on 16 GB machines and increase gradually while watching memory pressure.
- Batch prompts together when possible so that MLX can reuse model weights already staged in unified memory.

## Place models on fast local storage
- Keep `OLLAMA_MODELS` on an internal SSD or a fast external NVMe drive. Slow disks increase load latency and can starve the GPU of data.
- Avoid network filesystems for the model directory to keep I/O predictable during load and generation.

## Warm the runner between requests
- For repeated use of the same model, keep the runner warm (e.g., via `ollmlx run` in another terminal). Avoiding process cold-starts reduces the time spent reinitializing Metal contexts and memory pools.

## Monitor system health during long runs
- Watch **Activity Monitor → Memory** to ensure swap usage stays low. If swap rises, lower the context length or switch to a smaller/quantized model.
- Track GPU utilization and temperature with `powermetrics --samplers smc -n 1` to confirm the GPU is fully engaged without thermal throttling.
