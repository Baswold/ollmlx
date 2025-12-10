package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/llm"
)

// PullMLXModel downloads an MLX model from HuggingFace
func PullMLXModel(ctx context.Context, modelName string, fn func(api.ProgressResponse)) error {
	slog.Info("pulling MLX model from HuggingFace", "model", modelName)

	manager := llm.NewMLXModelManager()

	// Check if model already exists
	if manager.ModelExists(modelName) {
		fn(api.ProgressResponse{
			Status: fmt.Sprintf("model %s already exists", modelName),
		})
		return nil
	}

	// Download the model
	fn(api.ProgressResponse{
		Status: fmt.Sprintf("pulling MLX model %s from HuggingFace", modelName),
	})

	err := manager.DownloadMLXModel(modelName, func(status string, progress float64) {
		fn(api.ProgressResponse{
			Status:    status,
			Completed: int64(progress),
			Total:     100,
		})
	})

	if err != nil {
		return fmt.Errorf("failed to download MLX model: %w", err)
	}

	fn(api.ProgressResponse{
		Status: "success",
	})

	return nil
}

// ListMLXModels returns all locally cached MLX models
func ListMLXModels() ([]api.ListModelResponse, error) {
	manager := llm.NewMLXModelManager()

	mlxModels, err := manager.ListModels()
	if err != nil {
		return nil, err
	}

	var models []api.ListModelResponse
	for _, m := range mlxModels {
		models = append(models, api.ListModelResponse{
			Model:      m.Name,
			Name:       m.Name,
			Size:       m.Size,
			Digest:     m.Digest,
			ModifiedAt: m.ModifiedAt,
			Details: api.ModelDetails{
				Format:            "MLX",
				Family:            m.Family,
				ParameterSize:     m.ParameterSize,
				QuantizationLevel: m.QuantizLevel,
			},
		})
	}

	return models, nil
}

// ShowMLXModel returns metadata for a specific MLX model
func ShowMLXModel(modelName string) (*api.ShowResponse, error) {
	manager := llm.NewMLXModelManager()

	info, err := manager.GetModelInfo(modelName)
	if err != nil {
		return nil, err
	}

	return &api.ShowResponse{
		ModelInfo: api.ModelInfo{
			Format:            "MLX",
			Family:            info.Family,
			ParameterSize:     info.ParameterSize,
			QuantizationLevel: info.QuantizLevel,
		},
		ModifiedAt: info.ModifiedAt,
		Size:       info.Size,
		Digest:     info.Digest,
		Details: api.ModelDetails{
			Format:            "MLX",
			Family:            info.Family,
			ParameterSize:     info.ParameterSize,
			QuantizationLevel: info.QuantizLevel,
		},
	}, nil
}

// DeleteMLXModel removes an MLX model from local storage
func DeleteMLXModel(modelName string) error {
	manager := llm.NewMLXModelManager()
	return manager.DeleteModel(modelName)
}

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
	manager := llm.NewMLXModelManager()
	return manager.ModelExists(modelName)
}
