package llm

import (
	"os"
	"path/filepath"
	"strings"
)

// ModelFormat represents the format of a model file
type ModelFormat int

const (
	ModelFormatUnknown ModelFormat = iota
	ModelFormatGGUF
	ModelFormatMLX
)

// String returns the string representation of the model format
func (f ModelFormat) String() string {
	switch f {
	case ModelFormatGGUF:
		return "GGUF"
	case ModelFormatMLX:
		return "MLX"
	default:
		return "Unknown"
	}
}

// DetectModelFormat determines the format of a model based on its path and metadata
func DetectModelFormat(modelPath string) ModelFormat {
	// Check file extension first
	ext := strings.ToLower(filepath.Ext(modelPath))

	// GGUF files have .gguf extension
	if ext == ".gguf" {
		return ModelFormatGGUF
	}

	// Check if it's a directory (typical for MLX models)
	info, err := os.Stat(modelPath)
	if err == nil && info.IsDir() {
		// MLX models are typically directories containing:
		// - config.json
		// - model.safetensors or weights.npz
		// - tokenizer.json or tokenizer.model

		// Check for MLX model indicators
		configPath := filepath.Join(modelPath, "config.json")
		safetensorsPath := filepath.Join(modelPath, "model.safetensors")
		weightsPath := filepath.Join(modelPath, "weights.npz")

		if fileExists(configPath) && (fileExists(safetensorsPath) || fileExists(weightsPath)) {
			return ModelFormatMLX
		}
	}

	// Check for Hugging Face model names (e.g., "mlx-community/Llama-3.2-3B-Instruct-4bit")
	// These should be treated as MLX models that need to be downloaded
	if strings.Contains(modelPath, "/") && !filepath.IsAbs(modelPath) {
		// Likely a HuggingFace model reference
		// MLX community models are prefixed with "mlx-community/"
		if strings.HasPrefix(modelPath, "mlx-community/") {
			return ModelFormatMLX
		}

		// Check if it's a known MLX model pattern
		if strings.Contains(strings.ToLower(modelPath), "-mlx") {
			return ModelFormatMLX
		}
	}

	// If we can't determine, default to GGUF for backward compatibility
	return ModelFormatGGUF
}

// IsMLXModel is a convenience function to check if a model is MLX format
func IsMLXModel(modelPath string) bool {
	return DetectModelFormat(modelPath) == ModelFormatMLX
}

// IsGGUFModel is a convenience function to check if a model is GGUF format
func IsGGUFModel(modelPath string) bool {
	return DetectModelFormat(modelPath) == ModelFormatGGUF
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
