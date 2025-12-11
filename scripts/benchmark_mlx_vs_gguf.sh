#!/bin/bash

# MLX vs GGUF Performance Benchmark Script
# Compares generation speed and quality between MLX and GGUF models

echo "=== MLX vs GGUF Performance Benchmark ==="
echo "Date: $(date)"
echo "System: $(uname -srm)"
echo ""

# Configuration
MODEL_NAME="Llama-3.2-1B-Instruct"
PROMPT="Write a detailed explanation of how neural networks work, including their architecture, training process, and applications in modern AI systems."
WARMUP_ITERATIONS=1
BENCHMARK_ITERATIONS=3
TIMEOUT=120

# Check if both ollmlx and ollama are available
if ! command -v ./ollmlx &> /dev/null; then
    echo "❌ Error: ollmlx binary not found in current directory"
    exit 1
fi

if ! command -v ollama &> /dev/null; then
    echo "⚠️  Warning: ollama binary not found. GGUF benchmarking will be skipped."
    OLLAMA_AVAILABLE=false
else
    OLLAMA_AVAILABLE=true
fi

echo "=== System Information ==="
echo "CPU: $(sysctl -n machdep.cpu.brand_string)"
echo "Memory: $(sysctl -n hw.memsize | awk '{printf "%.1f GB", $1/1024/1024/1024}')"
echo ""

# Function to benchmark a model
benchmark_model() {
    local model_type=$1
    local model_name=$2
    local is_mlx=$3
    
    echo "=== Benchmarking ${model_type} Model: ${model_name} ==="
    
    # Warmup (not measured)
    echo "Warming up..."
    for i in $(seq 1 $WARMUP_ITERATIONS); do
        if [ "$is_mlx" = true ]; then
            echo "$PROMPT" | ./ollmlx run "$model_name" --timeout 30 > /dev/null 2>&1
        else
            echo "$PROMPT" | ollama run "$model_name" --timeout 30 > /dev/null 2>&1
        fi
    done
    
    # Actual benchmark
    echo "Running benchmark..."
    
    total_tokens=0
    total_time=0
    successful_runs=0
    
    for i in $(seq 1 $BENCHMARK_ITERATIONS); do
        echo "  Run $i/$BENCHMARK_ITERATIONS..."
        
        start_time=$(date +%s.%N)
        
        if [ "$is_mlx" = true ]; then
            # MLX generation
            output=$(echo "$PROMPT" | ./ollmlx run "$model_name" --timeout $TIMEOUT 2>&1)
            exit_code=$?
        else
            # GGUF generation  
            output=$(echo "$PROMPT" | ollama run "$model_name" --timeout $TIMEOUT 2>&1)
            exit_code=$?
        fi
        
        end_time=$(date +%s.%N)
        
        if [ $exit_code -eq 0 ]; then
            # Count tokens (approximate by word count)
            token_count=$(echo "$output" | wc -w)
            
            # Calculate time
            run_time=$(echo "$end_time - $start_time" | bc)
            
            total_tokens=$((total_tokens + token_count))
            total_time=$(echo "$total_time + $run_time" | bc)
            successful_runs=$((successful_runs + 1))
            
            echo "    ✓ Success: ${token_count} tokens in ${run_time} seconds"
        else
            echo "    ✗ Failed: Exit code $exit_code"
        fi
    done
    
    if [ $successful_runs -gt 0 ]; then
        avg_tokens=$((total_tokens / successful_runs))
        avg_time=$(echo "scale=3; $total_time / $successful_runs" | bc)
        tokens_per_sec=$(echo "scale=1; $avg_tokens / $avg_time" | bc)
        
        echo ""
        echo "Results:"
        echo "  Successful runs: $successful_runs/$BENCHMARK_ITERATIONS"
        echo "  Average tokens: $avg_tokens"
        echo "  Average time: ${avg_time}s"
        echo "  Tokens/second: ${tokens_per_sec}"
        echo ""
        
        # Return performance data
        echo "$model_type,$model_name,$avg_tokens,$avg_time,$tokens_per_sec"
    else
        echo "❌ All runs failed for $model_type model"
        echo "$model_type,$model_name,0,0,0"
    fi
}

# Main benchmark execution
echo "=== Starting Benchmarks ==="
echo ""

# Results header
results_file="benchmark_results_$(date +%Y%m%d_%H%M%S).csv"
echo "Type,Model,AvgTokens,AvgTime,TokensPerSec" > "$results_file"

# Benchmark GGUF (if available)
if [ "$OLLAMA_AVAILABLE" = true ]; then
    echo "Checking GGUF model availability..."
    if ollama list | grep -q "$MODEL_NAME"; then
        echo "GGUF model found: $MODEL_NAME"
        gguf_result=$(benchmark_model "GGUF" "$MODEL_NAME" false)
        echo "$gguf_result" >> "$results_file"
    else
        echo "Pulling GGUF model: $MODEL_NAME"
        ollama pull "$MODEL_NAME" --timeout 300
        if [ $? -eq 0 ]; then
            gguf_result=$(benchmark_model "GGUF" "$MODEL_NAME" false)
            echo "$gguf_result" >> "$results_file"
        else
            echo "❌ Failed to pull GGUF model"
        fi
    fi
else
    echo "Skipping GGUF benchmark (ollama not available)"
fi

# Benchmark MLX
mlx_model="mlx-community/${MODEL_NAME}-4bit"
echo "Checking MLX model availability..."

if ./ollmlx list | grep -q "mlx-community_${MODEL_NAME//-/_}"; then
    echo "MLX model found: $mlx_model"
    mlx_result=$(benchmark_model "MLX" "$mlx_model" true)
    echo "$mlx_result" >> "$results_file"
else
    echo "Pulling MLX model: $mlx_model"
    ./ollmlx pull "$mlx_model" --timeout 300
    if [ $? -eq 0 ]; then
        mlx_result=$(benchmark_model "MLX" "$mlx_model" true)
        echo "$mlx_result" >> "$results_file"
    else
        echo "❌ Failed to pull MLX model"
    fi
fi

echo ""
echo "=== Benchmark Complete ==="
echo ""
echo "Results saved to: $results_file"
echo ""
echo "Summary:"
cat "$results_file"

echo ""
echo "=== Performance Analysis ==="

# Calculate speedup if both results available
if [ -n "$gguf_result" ] && [ -n "$mlx_result" ]; then
    gguf_tps=$(echo "$gguf_result" | cut -d',' -f5)
    mlx_tps=$(echo "$mlx_result" | cut -d',' -f5)
    
    if [ "$gguf_tps" != "0" ] && [ "$mlx_tps" != "0" ]; then
        speedup=$(echo "scale=2; $mlx_tps / $gguf_tps" | bc)
        echo "MLX Speedup: ${speedup}x faster than GGUF"
        
        if (( $(echo "$speedup > 2.0" | bc -l) )); then
            echo "✅ Performance target achieved (2-3x faster)"
        else
            echo "⚠️  Performance below target"
        fi
    fi
fi

echo ""
echo "=== Recommendations ==="
echo "1. Run multiple iterations for statistical significance"
echo "2. Test with different model sizes"
echo "3. Compare memory usage with Activity Monitor"
echo "4. Test with streaming vs non-streaming modes"
echo "5. Validate response quality, not just speed"

echo ""
echo "Benchmark script completed."