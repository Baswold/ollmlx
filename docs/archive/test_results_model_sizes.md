# MLX Model Sizes Test Results

## Test Environment

- **Date:** 2025-12-11
- **System:** macOS (Apple Silicon)
- **Ollmlx Version:** 0.13.2
- **Available MLX Models:**
  - `mlx-community_gemma-3-270m-4bit` (234MB)
  - `mlx_community_gemma_2_2b_it_4bit` (~1.5GB)
  - `gemma-3-270m-4bit` (839MB)

## Test Overview

**Objective:** Test MLX generation with different model sizes to ensure stability and performance across the spectrum.

**Current Status:** MLX generation not functional (see `test_results_mlx_generation.md` for details)

## Test 1: Tiny Model (135M Parameters)

### Model: SmolLM2-135M-Instruct-4bit

**Expected Size:** ~150MB
**Use Case:** Fast testing, quick prototyping

**Test Command:**
```bash
./ollmlx pull mlx-community/SmolLM2-135M-Instruct-4bit
echo "Hello!" | ./ollmlx run mlx-community/SmolLM2-135M-Instruct-4bit
```

**Result:** ⚠️ NOT TESTED (MLX generation not working)

**Notes:**
- Would be ideal for rapid testing due to small size
- Should load quickly and generate fast responses
- Good candidate for CI/CD testing

## Test 2: Small Model (1B Parameters)

### Model: Llama-3.2-1B-Instruct-4bit

**Expected Size:** ~1.2GB
**Use Case:** Balanced performance and quality

**Test Command:**
```bash
./ollmlx pull mlx-community/Llama-3.2-1B-Instruct-4bit
echo "Explain recursion" | ./ollmlx run mlx-community/Llama-3.2-1B-Instruct-4bit
```

**Result:** ⚠️ NOT TESTED (MLX generation not working)

**Notes:**
- Good balance between speed and quality
- Should handle most tasks reasonably well
- Manageable memory footprint

## Test 3: Medium Model (3B Parameters)

### Model: Llama-3.2-3B-Instruct-4bit

**Expected Size:** ~3.5GB
**Use Case:** High-quality responses, production use

**Test Command:**
```bash
./ollmlx pull mlx-community/Llama-3.2-3B-Instruct-4bit
echo "Write Python code for Fibonacci" | ./ollmlx run mlx-community/Llama-3.2-3B-Instruct-4bit
```

**Result:** ⚠️ NOT TESTED (MLX generation not working)

**Notes:**
- Should provide high-quality responses
- May be slower but more accurate
- Higher memory requirements

## Test 4: Available Model - Gemma 3 270M

### Model: mlx-community_gemma-3-270m-4bit (Already Downloaded)

**Size:** 234MB
**Parameters:** 270M
**Quantization:** 4-bit

**Test Command:**
```bash
echo "Why is the sky blue?" | ./ollmlx run mlx-community/gemma-3-270m-4bit
```

**Result:** ❌ FAILED (Same as previous tests)

**Error:** `model 'mlx-community/gemma-3-270m-4bit' not found`

**Analysis:**
- Model exists in cache
- Same routing issue as before
- MLX backend not starting
- Consistent behavior across all model sizes

## Test 5: Available Model - Gemma 2 2B

### Model: mlx_community_gemma_2_2b_it_4bit (Already Downloaded)

**Size:** ~1.5GB
**Parameters:** 2B
**Quantization:** 4-bit

**Test Command:**
```bash
echo "Explain quantum computing" | ./ollmlx run mlx_community_gemma_2_2b_it_4bit
```

**Result:** ❌ FAILED (Same routing issue)

**Error:** `model 'mlx_community_gemma_2_2b_it_4bit' not found`

**Analysis:**
- Larger model, same issue
- Confirms problem is not size-related
- Issue is in MLX routing logic

## Performance Expectations (Theoretical)

Based on MLX framework capabilities and Apple Silicon optimization:

| Model Size | Expected Tokens/Sec | Memory Usage | Use Case |
|------------|---------------------|--------------|----------|
| 135M | 50-100 | 500MB | Rapid testing |
| 1B | 30-60 | 1.5GB | Balanced use |
| 3B | 15-30 | 3.5GB | High quality |
| 7B | 5-15 | 7GB | Production |

**Note:** These are theoretical estimates. Actual performance would need to be measured once MLX generation is working.

## Issues Found

### Consistent Issues Across All Sizes

1. **Routing Problem:** All model sizes fail with same error
   - Not a size-specific issue
   - MLX backend never starts regardless of model size

2. **Model Detection:** Detection logic fails for all MLX models
   - Not related to model parameters or size
   - Fundamental routing issue

3. **Error Consistency:** Same 404 error for all models
   - Confirms single root cause
   - Not multiple independent issues

## Recommendations

### Immediate Actions

1. **Fix MLX Routing:** Resolve the core routing issue first
   - Apply fix from `test_results_mlx_generation.md`
   - Test with smallest model first

2. **Test Progression:** Once fixed, test in this order:
   ```bash
   # 1. Tiny model (fastest to test)
   ./ollmlx pull mlx-community/SmolLM2-135M-Instruct-4bit
   
   # 2. Small model
   ./ollmlx pull mlx-community/Llama-3.2-1B-Instruct-4bit
   
   # 3. Medium model  
   ./ollmlx pull mlx-community/Llama-3.2-3B-Instruct-4bit
   ```

### Performance Testing Plan

Once MLX generation works:

```bash
# Create benchmark script
cat > benchmark_mlx.sh << 'EOF'
#!/bin/bash

MODELS=(
    "mlx-community/SmolLM2-135M-Instruct-4bit"
    "mlx-community/Llama-3.2-1B-Instruct-4bit" 
    "mlx-community/Llama-3.2-3B-Instruct-4bit"
)

PROMPT="Write a detailed explanation of how neural networks work."

for model in "${MODELS[@]}"; do
    echo "=== Benchmarking $model ==="
    
    # Pull model (skip if exists)
    ./ollmlx pull "$model" 2>/dev/null || true
    
    # Test generation with timing
    start=$(date +%s)
    output=$(echo "$PROMPT" | ./ollmlx run "$model" --timeout 60)
    end=$(date +%s)
    
    duration=$((end - start))
    tokens=$(echo "$output" | wc -w)
    tps=$((tokens / duration))
    
    echo "Time: ${duration}s"
    echo "Tokens: ${tokens}"
    echo "Tokens/sec: ${tps}"
    echo ""
done
EOF

chmod +x benchmark_mlx.sh
./benchmark_mlx.sh
```

## Conclusion

**Current Status:** Cannot test different model sizes due to MLX routing issue

**Root Cause:** Single routing problem affects all MLX models regardless of size

**Priority:** Fix MLX routing first, then test performance across sizes

**Expected Outcome:** Once fixed, should see:
- ✅ All model sizes work
- ✅ Performance scales with model size (smaller = faster)
- ✅ Memory usage proportional to model size
- ✅ Quality improves with larger models

**Next Steps:**
1. Apply routing fix from main test results
2. Test with smallest model first
3. Progress to larger models
4. Document performance characteristics

**Time Estimate:** 1-2 hours to fix and test all sizes