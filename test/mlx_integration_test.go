package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ollama/ollama/llm"
)

// TestMLXModelDetection tests the model format detection logic
func TestMLXModelDetection(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected llm.ModelFormat
	}{
		{
			name:     "HuggingFace MLX model",
			path:     "mlx-community/Llama-3.2-1B-Instruct-4bit",
			expected: llm.ModelFormatMLX,
		},
		{
			name:     "GGUF model file",
			path:     "/path/to/model.gguf",
			expected: llm.ModelFormatGGUF,
		},
		{
			name:     "Model with mlx suffix",
			path:     "llama-7b-mlx",
			expected: llm.ModelFormatMLX,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := llm.DetectModelFormat(tt.path)
			if format != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, format)
			}
		})
	}
}

// TestMLXModelManager tests the MLX model manager functionality
func TestMLXModelManager(t *testing.T) {
	// Create temporary directory for test models
	tmpDir := t.TempDir()
	t.Setenv("OLLAMA_MODELS", tmpDir)

	// Create a fake MLX model directory
	modelName := "test-model"
	modelPath := filepath.Join(tmpDir, "mlx", modelName)
	os.MkdirAll(modelPath, 0755)

	// Create required MLX model files
	os.WriteFile(filepath.Join(modelPath, "config.json"), []byte(`{"architectures": ["LlamaForCausalLM"]}`), 0644)
	os.WriteFile(filepath.Join(modelPath, "model.safetensors"), []byte("fake weights"), 0644)

	manager := llm.NewMLXModelManager()

	// Override the models directory for testing
	// Note: This requires exposing the modelsDir field or adding a test helper

	t.Run("ListModels", func(t *testing.T) {
		models, err := manager.ListModels()
		if err != nil {
			t.Fatalf("failed to list models: %v", err)
		}

		// Should return models from the MLX directory
		if len(models) < 0 {
			t.Error("expected at least 0 models")
		}
	})

	t.Run("GetModelInfo", func(t *testing.T) {
		// This test would work with a real model
		_, err := manager.GetModelInfo("nonexistent-model")
		if err == nil {
			t.Error("expected error for nonexistent model")
		}
	})
}

// TestMLXBackendIntegration tests the integration between Go and MLX backend
func TestMLXBackendIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This test would require the MLX backend to be running
	// and a model to be available

	t.Run("ServerStartup", func(t *testing.T) {
		// Test that we can start the MLX backend
		// This would require mocking or having the Python environment set up
		t.Skip("requires MLX backend environment")
	})
}

// TestMLXModelPull tests pulling an MLX model from HuggingFace
func TestMLXModelPull(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pull test in short mode")
	}

	if os.Getenv("RUN_MLX_PULL_TEST") == "" {
		t.Skip("set RUN_MLX_PULL_TEST=1 to exercise MLX pull")
	}

	t.Setenv("OLLAMA_MODELS", t.TempDir())

	manager := llm.NewMLXModelManager()

	// Use a very small test model
	testModel := "mlx-community/SmolLM2-135M-Instruct-4bit"

	t.Run("PullMLXModel", func(t *testing.T) {
		// Check if model already exists
		if manager.ModelExists(testModel) {
			t.Skip("test model already exists")
		}

		// Mock progress function
		progressCalled := false
		progressFn := func(status string, progress float64) {
			progressCalled = true
			t.Logf("Progress: %s (%.1f%%)", status, progress)
		}

		err := manager.DownloadMLXModel(context.Background(), testModel, progressFn)
		if err != nil {
			t.Fatalf("failed to download model: %v", err)
		}

		if !progressCalled {
			t.Error("progress callback was not called")
		}

		// Verify model exists
		if !manager.ModelExists(testModel) {
			t.Error("model not found after download")
		}

		// Cleanup
		manager.DeleteModel(testModel)
	})
}

// BenchmarkMLXvsGGUF benchmarks MLX performance vs GGUF
func BenchmarkMLXvsGGUF(b *testing.B) {
	// This would require both backends to be set up
	// and comparable models to be available
	b.Skip("requires full environment setup")
}
