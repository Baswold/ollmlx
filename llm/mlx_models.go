package llm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ollama/ollama/envconfig"
)

// MLXModelInfo represents metadata about an MLX model
type MLXModelInfo struct {
	Name           string    `json:"name"`
	Size           int64     `json:"size"`
	Digest         string    `json:"digest"`
	ModifiedAt     time.Time `json:"modified_at"`
	Format         string    `json:"format"`
	Family         string    `json:"family"`
	ParameterSize  string    `json:"parameter_size"`
	QuantizLevel   string    `json:"quantization_level"`
	LocalPath      string    `json:"-"`
	HuggingFaceURL string    `json:"huggingface_url,omitempty"`
}

// MLXModelManager handles MLX model storage and retrieval
type MLXModelManager struct {
	modelsDir string
}

// NewMLXModelManager creates a new MLX model manager
func NewMLXModelManager() *MLXModelManager {
	// Use Ollama's model directory structure respecting environment overrides
	modelsDir := filepath.Join(envconfig.Models(), "mlx")
	os.MkdirAll(modelsDir, 0755)

	return &MLXModelManager{
		modelsDir: modelsDir,
	}
}

// GetModelsDir returns the directory where MLX models are stored
func (m *MLXModelManager) GetModelsDir() string {
	return m.modelsDir
}

// ListModels returns all locally cached MLX models
func (m *MLXModelManager) ListModels() ([]MLXModelInfo, error) {
	var models []MLXModelInfo

	entries, err := os.ReadDir(m.modelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return models, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		modelPath := filepath.Join(m.modelsDir, entry.Name())
		info, err := m.GetModelInfo(entry.Name())
		if err != nil {
			// Skip invalid models
			continue
		}

		info.LocalPath = modelPath
		models = append(models, info)
	}

	return models, nil
}

// GetModelInfo retrieves metadata for a specific MLX model
func (m *MLXModelManager) GetModelInfo(modelName string) (MLXModelInfo, error) {
	modelPath := m.GetModelPath(modelName)

	info := MLXModelInfo{
		Name:   modelName,
		Format: "MLX",
	}

	// Check if model exists
	stat, err := os.Stat(modelPath)
	if err != nil {
		return info, err
	}

	info.ModifiedAt = stat.ModTime()

	// Try to read config.json for metadata
	configPath := filepath.Join(modelPath, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var config map[string]interface{}
		if err := json.Unmarshal(data, &config); err == nil {
			// Extract model family and size from config
			if arch, ok := config["architectures"].([]interface{}); ok && len(arch) > 0 {
				info.Family = fmt.Sprintf("%v", arch[0])
			}
			if hiddenSize, ok := config["hidden_size"].(float64); ok {
				// Rough estimate of parameter count from hidden size
				params := int(hiddenSize * 1000 / 1024) // Very rough approximation
				info.ParameterSize = fmt.Sprintf("%dM", params)
			}
		}
	}

	// Calculate total size
	size, err := m.calculateDirSize(modelPath)
	if err == nil {
		info.Size = size
	}

	// Generate a stable digest from the model name
	sum := sha256.Sum256([]byte(modelName))
	info.Digest = fmt.Sprintf("sha256:%x", sum)

	return info, nil
}

// GetModelPath returns the local path for a model name
func (m *MLXModelManager) GetModelPath(modelName string) string {
	// Handle both simple names and HuggingFace-style names
	// e.g., "llama-3-8b" or "mlx-community/Llama-3-8B-Instruct-4bit"

	// Convert HuggingFace URL format to local directory name
	localName := strings.ReplaceAll(modelName, "/", "_")

	return filepath.Join(m.modelsDir, localName)
}

// ModelExists checks if a model is already cached locally
func (m *MLXModelManager) ModelExists(modelName string) bool {
	modelPath := m.GetModelPath(modelName)

	// Check for required MLX model files
	configPath := filepath.Join(modelPath, "config.json")
	if _, err := os.Stat(configPath); err != nil {
		return false
	}

	// Check for model weights (either safetensors or npz)
	safetensorsPath := filepath.Join(modelPath, "model.safetensors")
	weightsPath := filepath.Join(modelPath, "weights.npz")

	_, err1 := os.Stat(safetensorsPath)
	_, err2 := os.Stat(weightsPath)

	return err1 == nil || err2 == nil
}

// DeleteModel removes a model from local storage
func (m *MLXModelManager) DeleteModel(modelName string) error {
	modelPath := m.GetModelPath(modelName)
	return os.RemoveAll(modelPath)
}

// calculateDirSize calculates the total size of a directory
func (m *MLXModelManager) calculateDirSize(path string) (int64, error) {
	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}

// HuggingFaceModelInfo represents model information from HuggingFace API
type HuggingFaceModelInfo struct {
	ModelID     string   `json:"modelId"`
	Author      string   `json:"author"`
	Downloads   int      `json:"downloads"`
	Tags        []string `json:"tags"`
	LastUpdated string   `json:"lastModified"`
}

// SearchMLXModels searches HuggingFace for MLX models
func SearchMLXModels(query string, limit int) ([]HuggingFaceModelInfo, error) {
	// Search HuggingFace for models with MLX tag
	url := fmt.Sprintf("https://huggingface.co/api/models?search=%s&filter=mlx&limit=%d", query, limit)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to search HuggingFace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HuggingFace API returned status %d", resp.StatusCode)
	}

	var models []HuggingFaceModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("failed to parse HuggingFace response: %w", err)
	}

	return models, nil
}

// DownloadMLXModel downloads an MLX model from HuggingFace
func (m *MLXModelManager) DownloadMLXModel(ctx context.Context, modelID string, progressFn func(string, float64)) error {
	// This is a simplified version - in production, you'd want to:
	// 1. Use the HuggingFace hub API to list files
	// 2. Download each file with proper progress tracking
	// 3. Verify checksums
	// 4. Handle resume on failure

	modelPath := m.GetModelPath(modelID)

	// Create model directory
	if err := os.MkdirAll(modelPath, 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(modelPath)
		}
	}()

	// Required files for an MLX model
	requiredFiles := []string{
		"config.json",
		"tokenizer.json",
		"tokenizer_config.json",
	}

	// Optional but common files
	optionalFiles := []string{
		"model.safetensors",
		"weights.npz",
		"special_tokens_map.json",
		"generation_config.json",
	}

	allFiles := append(requiredFiles, optionalFiles...)

	// Base URL for HuggingFace model files
	baseURL := fmt.Sprintf("https://huggingface.co/%s/resolve/main", modelID)

	totalFiles := len(allFiles)
	downloadedFiles := 0

	client := &http.Client{Timeout: 10 * time.Minute}

	updateProgress := func(status string, completed int) {
		if progressFn == nil {
			return
		}
		pct := (float64(completed) / float64(totalFiles)) * 100
		progressFn(status, math.Round(pct))
	}

	for _, filename := range allFiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		fileURL := fmt.Sprintf("%s/%s", baseURL, filename)
		destPath := filepath.Join(modelPath, filename)

		updateProgress(fmt.Sprintf("downloading %s", filename), downloadedFiles)

		// Download file
		if err := m.downloadFile(ctx, client, fileURL, destPath); err != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			// If it's a required file, fail
			isRequired := false
			for _, req := range requiredFiles {
				if req == filename {
					isRequired = true
					break
				}
			}

			if isRequired {
				return fmt.Errorf("failed to download required file %s: %w", filename, err)
			}
			// Optional files can fail silently
		}

		downloadedFiles++
		updateProgress(fmt.Sprintf("downloaded %s", filename), downloadedFiles)
	}

	if progressFn != nil {
		progressFn("Download complete", 100)
	}

	cleanup = false

	return nil
}

// downloadFile downloads a file from a URL to a local path
func (m *MLXModelManager) downloadFile(ctx context.Context, client *http.Client, url, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	tmpPath := destPath + ".part"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, resp.Body); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}

	return nil
}

// GetPopularMLXModels returns a curated list of popular/recommended MLX models
func GetPopularMLXModels() []string {
	return []string{
		"mlx-community/Llama-3.2-3B-Instruct-4bit",
		"mlx-community/Llama-3.2-1B-Instruct-4bit",
		"mlx-community/Mistral-7B-Instruct-v0.3-4bit",
		"mlx-community/Qwen2.5-7B-Instruct-4bit",
		"mlx-community/SmolLM2-1.7B-Instruct-4bit",
		"mlx-community/Phi-3.5-mini-instruct-4bit",
		"mlx-community/gemma-2-2b-it-4bit",
	}
}
