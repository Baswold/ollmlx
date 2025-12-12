#!/usr/bin/env python3
"""Run MLX and GGUF generation benchmarks with latency statistics.

This script runs multiple generations against both the MLX backend (via the
`ollmlx` CLI) and the GGUF backend (via the `ollama` CLI) and reports
throughput plus average and tail latencies.
"""

from __future__ import annotations

import argparse
import json
import math
import os
import shutil
import statistics
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, List, Optional


@dataclass
class BenchmarkResult:
    label: str
    model: str
    durations: List[float]
    tokens: List[int]

    @property
    def successful_runs(self) -> int:
        return len(self.durations)

    @property
    def average_latency(self) -> Optional[float]:
        if not self.durations:
            return None
        return statistics.mean(self.durations)

    @property
    def p50_latency(self) -> Optional[float]:
        return _percentile(self.durations, 0.50)

    @property
    def p95_latency(self) -> Optional[float]:
        return _percentile(self.durations, 0.95)

    @property
    def p99_latency(self) -> Optional[float]:
        return _percentile(self.durations, 0.99)

    @property
    def total_tokens(self) -> int:
        return sum(self.tokens)

    @property
    def total_time(self) -> float:
        return sum(self.durations)

    @property
    def tokens_per_second(self) -> Optional[float]:
        if not self.durations or self.total_time == 0:
            return None
        return self.total_tokens / self.total_time

    def to_dict(self) -> dict:
        return {
            "label": self.label,
            "model": self.model,
            "successful_runs": self.successful_runs,
            "average_latency_ms": _format_ms(self.average_latency),
            "p50_latency_ms": _format_ms(self.p50_latency),
            "p95_latency_ms": _format_ms(self.p95_latency),
            "p99_latency_ms": _format_ms(self.p99_latency),
            "tokens_per_second": round(self.tokens_per_second, 2) if self.tokens_per_second else None,
            "total_tokens": self.total_tokens,
        }


def _format_ms(value: Optional[float]) -> Optional[float]:
    if value is None:
        return None
    return round(value * 1000, 2)


def _percentile(values: Iterable[float], percentile: float) -> Optional[float]:
    values = list(values)
    if not values:
        return None
    if len(values) == 1:
        return values[0]
    sorted_values = sorted(values)
    index = (len(sorted_values) - 1) * percentile
    lower = math.floor(index)
    upper = math.ceil(index)
    if lower == upper:
        return sorted_values[int(index)]
    return sorted_values[lower] + (sorted_values[upper] - sorted_values[lower]) * (index - lower)


def run_command(command: List[str], prompt: str, timeout: int) -> tuple[Optional[float], Optional[int]]:
    start = time.perf_counter()
    try:
        completed = subprocess.run(
            command,
            input=prompt.encode(),
            capture_output=True,
            timeout=timeout,
            check=True,
        )
    except subprocess.CalledProcessError as exc:
        sys.stderr.write(
            f"Command {' '.join(command)} failed with {exc.returncode}: {exc.stderr.decode(errors='ignore')}\n"
        )
        return None, None
    except subprocess.TimeoutExpired:
        sys.stderr.write(f"Command {' '.join(command)} timed out after {timeout}s\n")
        return None, None

    duration = time.perf_counter() - start
    output = completed.stdout.decode(errors="ignore")
    token_count = len(output.split())
    return duration, token_count


def run_benchmark(
    label: str,
    model: str,
    command: List[str],
    prompt: str,
    iterations: int,
    warmup: int,
    timeout: int,
) -> BenchmarkResult:
    durations: List[float] = []
    tokens: List[int] = []

    if warmup:
        for _ in range(warmup):
            run_command(command, prompt, timeout)

    for attempt in range(1, iterations + 1):
        duration, token_count = run_command(command, prompt, timeout)
        if duration is None or token_count is None:
            sys.stderr.write(f"{label} run {attempt}/{iterations} failed; skipping in stats.\n")
            continue
        durations.append(duration)
        tokens.append(token_count)

    return BenchmarkResult(label=label, model=model, durations=durations, tokens=tokens)


def ensure_executable(path: Path, name: str) -> None:
    if path.is_file() and os.access(path, os.X_OK):
        return
    if shutil.which(name):
        return
    sys.exit(f"{name} not found. Please build it before running benchmarks.")


def pull_model(command: List[str], label: str, model: str) -> None:
    print(f"Ensuring {label} model {model} is available...")
    try:
        subprocess.run(command, check=True)
    except FileNotFoundError:
        sys.exit(f"Required binary for {label} is missing: {' '.join(command[:1])}")
    except subprocess.CalledProcessError as exc:
        sys.exit(f"Failed to pull {label} model {model}: {exc}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Benchmark MLX vs GGUF generation latencies")
    parser.add_argument("--mlx-model", default="mlx-community/Llama-3.2-1B-Instruct-4bit", help="Model identifier for MLX")
    parser.add_argument("--gguf-model", default="Llama-3.2-1B-Instruct", help="Model identifier for GGUF")
    parser.add_argument("--prompt", default="Explain the difference between latency and throughput in LLM inference.", help="Prompt to generate")
    parser.add_argument("--iterations", type=int, default=5, help="Number of measured runs per backend")
    parser.add_argument("--warmup", type=int, default=1, help="Number of warmup runs per backend")
    parser.add_argument("--timeout", type=int, default=180, help="Timeout for each generation (seconds)")
    parser.add_argument("--skip-gguf", action="store_true", help="Skip GGUF benchmarking")
    parser.add_argument("--output", type=Path, default=Path("benchmark-results.json"), help="Where to write JSON results")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    prompt = args.prompt

    results: List[BenchmarkResult] = []

    ensure_executable(Path("./ollmlx"), "ollmlx")
    if not args.skip_gguf:
        ensure_executable(Path("ollama"), "ollama")

    pull_model(["./ollmlx", "pull", args.mlx_model], "MLX", args.mlx_model)
    if not args.skip_gguf:
        pull_model(["ollama", "pull", args.gguf_model], "GGUF", args.gguf_model)

    print("Running MLX benchmark...")
    mlx_result = run_benchmark(
        label="MLX",
        model=args.mlx_model,
        command=["./ollmlx", "run", args.mlx_model, "--timeout", str(args.timeout)],
        prompt=prompt,
        iterations=args.iterations,
        warmup=args.warmup,
        timeout=args.timeout,
    )
    results.append(mlx_result)

    if not args.skip_gguf:
        print("Running GGUF benchmark...")
        gguf_result = run_benchmark(
            label="GGUF",
            model=args.gguf_model,
            command=["ollama", "run", args.gguf_model, "--timeout", str(args.timeout)],
            prompt=prompt,
            iterations=args.iterations,
            warmup=args.warmup,
            timeout=args.timeout,
        )
        results.append(gguf_result)

    payload = {
        "prompt": prompt,
        "iterations": args.iterations,
        "warmup": args.warmup,
        "timeout_seconds": args.timeout,
        "results": [result.to_dict() for result in results],
    }

    args.output.write_text(json.dumps(payload, indent=2))
    print(f"Saved benchmark results to {args.output}")

    for result in results:
        print(f"\n{result.label} ({result.model})")
        print(f"  Successful runs: {result.successful_runs}/{args.iterations}")
        if not result.durations:
            print("  No successful runs; see stderr for errors.")
            continue
        print(f"  Avg latency: {_format_ms(result.average_latency)} ms")
        print(f"  p50 latency: {_format_ms(result.p50_latency)} ms")
        print(f"  p95 latency: {_format_ms(result.p95_latency)} ms")
        print(f"  p99 latency: {_format_ms(result.p99_latency)} ms")
        tps = result.tokens_per_second
        print(f"  Tokens/sec: {tps:.2f}" if tps is not None else "  Tokens/sec: n/a")

    return 0


if __name__ == "__main__":
    sys.exit(main())
