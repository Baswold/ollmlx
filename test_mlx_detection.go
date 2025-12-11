//go:build debug
// +build debug

package main

import (
	"fmt"
	"strings"
)

// IsMLXModelReference checks if a model name is an MLX model reference
func IsMLXModelReference(modelName string) bool {
	// MLX models typically come from HuggingFace with format:
	// - "mlx-community/ModelName"
	// - contain "mlx" in the name
	// - or are stored in the MLX models directory

	if strings.HasPrefix(modelName, "mlx-community/") {
		return true
	}

	if strings.Contains(strings.ToLower(modelName), "-mlx") {
		return true
	}

	// Check if model exists in MLX cache
	// manager := llm.NewMLXModelManager()
	// return manager.ModelExists(modelName)
	return false // Simplified for testing
}

func main() {
	testModels := []string{
		"mlx-community/gemma-3-270m-4bit",
		"mlx-community_gemma-3-270m-4bit",
		"gemma-3-270m-4bit",
		"mlx-community/Llama-3-8B-Instruct-4bit",
		"some-model-mlx",
		"regular-model",
	}

	fmt.Println("=== MLX Model Detection Test ===")
	for _, model := range testModels {
		result := IsMLXModelReference(model)
		fmt.Printf("%-40s -> %v\n", model, result)
	}
}
