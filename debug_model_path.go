//go:build debug
// +build debug

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// Simulate the MLX model manager logic
	modelsDir := filepath.Join(os.Getenv("HOME"), ".ollama/models/mlx")

	testModels := []string{
		"mlx-community/gemma-3-270m-4bit",
		"mlx-community_gemma-3-270m-4bit",
		"gemma-3-270m-4bit",
	}

	fmt.Println("=== Model Path Debug ===")
	fmt.Printf("Models directory: %s\n\n", modelsDir)

	for _, modelName := range testModels {
		// Convert model name to local directory name (same as GetModelPath)
		localName := strings.ReplaceAll(modelName, "/", "_")
		modelPath := filepath.Join(modelsDir, localName)

		fmt.Printf("Model: %-35s -> Local: %-35s\n", modelName, localName)
		fmt.Printf("Path: %s\n", modelPath)

		// Check if directory exists
		if _, err := os.Stat(modelPath); err == nil {
			fmt.Printf("Status: ✓ Directory exists\n")
		} else {
			fmt.Printf("Status: ✗ Directory not found\n")
		}

		// Check for config.json
		configPath := filepath.Join(modelPath, "config.json")
		if _, err := os.Stat(configPath); err == nil {
			fmt.Printf("Config: ✓ config.json exists\n")
		} else {
			fmt.Printf("Config: ✗ config.json not found\n")
		}

		fmt.Println()
	}
}
