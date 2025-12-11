package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/llm"
)

// TestMLXBackendLoading tests loading an MLX model
func TestMLXBackendLoading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MLX test in short mode")
	}

	// Use a small test model
	testModel := "mlx-community/SmolLM2-135M-Instruct-4bit"

	// Check if model already exists
	manager := llm.NewMLXModelManager()
	if manager.ModelExists(testModel) {
		t.Log("Test model already exists, skipping download")
	} else {
		progressFn := func(status string, progress float64) {
			t.Logf("Download progress: %s (%.1f%%)", status, progress)
		}

		err := manager.DownloadMLXModel(context.Background(), testModel, progressFn)
		if err != nil {
			t.Logf("Failed to download test model (expected if offline): %v", err)
			t.Skip("cannot download test model")
		}
		defer func() {
			if err := manager.DeleteModel(testModel); err != nil {
				t.Logf("Failed to cleanup test model: %v", err)
			}
		}()
	}

	// Test model info retrieval
	info, err := manager.GetModelInfo(testModel)
	if err != nil {
		t.Fatalf("Failed to get model info: %v", err)
	}

	if info.Format != "MLX" {
		t.Errorf("Expected format MLX, got %s", info.Format)
	}

	if info.Name != testModel {
		t.Errorf("Expected model name %s, got %s", testModel, info.Name)
	}
}

// TestMLXCompletion tests text generation with MLX models
func TestMLXCompletion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MLX completion test in short mode")
	}

	// This test requires the ollama server to be running
	// and a model to be available

	// Check if server is running
	resp, err := http.Get("http://localhost:11434/api/version")
	if err != nil {
		t.Skip("ollama server not running, skipping test")
	}
	resp.Body.Close()

	// Use a small test model
	testModel := "mlx-community/SmolLM2-135M-Instruct-4bit"

	// Check if model exists
	manager := llm.NewMLXModelManager()
	if !manager.ModelExists(testModel) {
		t.Skipf("test model %s not available", testModel)
	}

	// Test completion endpoint
	client := &http.Client{}
	reqBody := map[string]interface{}{
		"model":  testModel,
		"prompt": "Hello",
		"stream": false,
		"options": map[string]interface{}{
			"temperature": 0.7,
			"max_tokens":  10,
		},
	}

	reqBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "http://localhost:11434/api/generate", strings.NewReader(string(reqBytes)))
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to call generate endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	var result api.GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Model != testModel {
		t.Errorf("Expected model %s, got %s", testModel, result.Model)
	}

	if result.Response == "" {
		t.Error("Expected non-empty response")
	}
}

// TestMLXStreaming tests streaming responses from MLX models
func TestMLXStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MLX streaming test in short mode")
	}

	// This test requires the ollama server to be running
	resp, err := http.Get("http://localhost:11434/api/version")
	if err != nil {
		t.Skip("ollama server not running, skipping test")
	}
	resp.Body.Close()

	// Use a small test model
	testModel := "mlx-community/SmolLM2-135M-Instruct-4bit"

	// Check if model exists
	manager := llm.NewMLXModelManager()
	if !manager.ModelExists(testModel) {
		t.Skipf("test model %s not available", testModel)
	}

	// Test streaming completion endpoint
	client := &http.Client{}
	reqBody := map[string]interface{}{
		"model":  testModel,
		"prompt": "Why is the sky blue?",
		"stream": true,
		"options": map[string]interface{}{
			"temperature": 0.7,
			"max_tokens":  20,
		},
	}

	reqBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "http://localhost:11434/api/generate", strings.NewReader(string(reqBytes)))
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to call generate endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Read streaming response
	var chunkCount int
	var lastChunk api.GenerateResponse

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Error reading stream: %v", err)
		}

		// Remove trailing newline
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse JSON line
		var chunk api.GenerateResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			t.Logf("Warning: failed to parse chunk: %v, line: %s", err, line)
			continue
		}

		chunkCount++
		lastChunk = chunk

		// Verify basic fields
		if chunk.Model != testModel {
			t.Errorf("Expected model %s, got %s", testModel, chunk.Model)
		}

		if chunk.Done && chunkCount < 2 {
			t.Error("Stream ended too early")
		}
	}

	if chunkCount == 0 {
		t.Error("Expected at least one chunk in streaming response")
	}

	if !lastChunk.Done {
		t.Error("Expected last chunk to have Done=true")
	}
}

// TestMLXModels tests listing and managing MLX models
func TestMLXModels(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MLX models test in short mode")
	}

	manager := llm.NewMLXModelManager()

	// Test listing models
	models, err := manager.ListModels()
	if err != nil {
		t.Fatalf("Failed to list models: %v", err)
	}

	t.Logf("Found %d MLX models", len(models))

	// Test model info
	for _, model := range models {
		info, err := manager.GetModelInfo(model.Name)
		if err != nil {
			t.Errorf("Failed to get info for model %s: %v", model.Name, err)
			continue
		}

		if info.Name != model.Name {
			t.Errorf("Model name mismatch: %s vs %s", info.Name, model.Name)
		}

		if info.Format != "MLX" {
			t.Errorf("Expected format MLX for model %s, got %s", info.Name, info.Format)
		}
	}
}

// TestMLXModelPull tests pulling an MLX model from HuggingFace
func TestMLXModelPull(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MLX pull test in short mode")
	}

	if os.Getenv("RUN_MLX_PULL_TEST") == "" {
		t.Skip("set RUN_MLX_PULL_TEST=1 to run MLX pull test")
	}

	// This test requires internet access
	_, err := http.Get("https://huggingface.co")
	if err != nil {
		t.Skip("no internet access, skipping pull test")
	}

	t.Setenv("OLLAMA_MODELS", t.TempDir())

	manager := llm.NewMLXModelManager()

	// Use a very small test model
	testModel := "mlx-community/SmolLM2-135M-Instruct-4bit"

	// Clean up if model exists
	if manager.ModelExists(testModel) {
		if err := manager.DeleteModel(testModel); err != nil {
			t.Logf("Warning: failed to cleanup existing model: %v", err)
		}
	}

	var progressCount int
	progressFn := func(status string, progress float64) {
		progressCount++
		if progressCount <= 5 || progressCount%10 == 0 { // Log first 5 and every 10th
			t.Logf("Download progress: %s (%.1f%%)", status, progress)
		}
	}

	err = manager.DownloadMLXModel(context.Background(), testModel, progressFn)
	if err != nil {
		t.Fatalf("Failed to download model: %v", err)
	}

	// Verify model exists
	if !manager.ModelExists(testModel) {
		t.Fatal("Model not found after download")
	}

	// Verify model info
	info, err := manager.GetModelInfo(testModel)
	if err != nil {
		t.Fatalf("Failed to get model info: %v", err)
	}

	if info.Format != "MLX" {
		t.Errorf("Expected format MLX, got %s", info.Format)
	}

	if info.Size == 0 {
		t.Error("Expected non-zero model size")
	}

	// Cleanup
	if err := manager.DeleteModel(testModel); err != nil {
		t.Logf("Warning: failed to cleanup test model: %v", err)
	}
}

// TestMLXvsGGUFCompatibility tests that MLX responses match GGUF format
func TestMLXvsGGUFCompatibility(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compatibility test in short mode")
	}

	// This test requires both backends to be available
	// and comparable models

	t.Skip("requires full environment setup with both MLX and GGUF models")
}

// BenchmarkMLXPerformance benchmarks MLX model performance
func BenchmarkMLXPerformance(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping MLX benchmark in short mode")
	}

	// This benchmark requires the ollama server to be running
	resp, err := http.Get("http://localhost:11434/api/version")
	if err != nil {
		b.Skip("ollama server not running, skipping benchmark")
	}
	resp.Body.Close()

	// Use a small test model
	testModel := "mlx-community/SmolLM2-135M-Instruct-4bit"

	// Check if model exists
	manager := llm.NewMLXModelManager()
	if !manager.ModelExists(testModel) {
		b.Skipf("test model %s not available", testModel)
	}

	// Warm up
	for i := 0; i < 5; i++ {
		if _, err := generateText(testModel, "Warm up"); err != nil {
			b.Skipf("generate warmup failed: %v", err)
		}
	}

	// Benchmark
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := generateText(testModel, "Why is the sky blue?"); err != nil {
			b.Fatalf("generate failed: %v", err)
		}
	}
}

// generateText generates text using the MLX model
func generateText(model, prompt string) (string, error) {
	client := &http.Client{}
	reqBody := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]interface{}{
			"temperature": 0.7,
			"max_tokens":  20,
		},
	}

	reqBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "http://localhost:11434/api/generate", strings.NewReader(string(reqBytes)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result api.GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Response, nil
}
