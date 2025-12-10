package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollama/ollama/api"
)

// MLXModelfile represents a Modelfile configuration for an MLX model
type MLXModelfile struct {
	// FROM specifies the MLX model source
	// Can be:
	// - HuggingFace path: "mlx-community/Llama-3.2-1B-Instruct-4bit"
	// - Local directory: "/path/to/mlx-model"
	From string

	// System prompt
	System string

	// Template for chat formatting
	Template string

	// Parameters
	Parameters map[string]interface{}

	// Adapter paths (LoRA)
	Adapters []string

	// License
	License string

	// Messages (few-shot examples)
	Messages []api.Message
}

// ParseMLXModelfile parses a Modelfile and extracts MLX-specific configuration
func ParseMLXModelfile(path string) (*MLXModelfile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	mf := &MLXModelfile{
		Parameters: make(map[string]interface{}),
	}

	lines := strings.Split(string(content), "\n")
	var currentCommand string
	var currentValue strings.Builder

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for command keywords
		if strings.HasPrefix(line, "FROM ") {
			mf.From = strings.TrimSpace(strings.TrimPrefix(line, "FROM "))
			continue
		}

		if strings.HasPrefix(line, "SYSTEM ") {
			currentCommand = "SYSTEM"
			currentValue.Reset()
			value := strings.TrimPrefix(line, "SYSTEM ")
			// Handle both inline and multiline
			if strings.HasPrefix(value, "\"\"\"") {
				currentValue.WriteString(strings.TrimPrefix(value, "\"\"\""))
			} else {
				mf.System = strings.Trim(value, "\"")
				currentCommand = ""
			}
			continue
		}

		if strings.HasPrefix(line, "TEMPLATE ") {
			currentCommand = "TEMPLATE"
			currentValue.Reset()
			value := strings.TrimPrefix(line, "TEMPLATE ")
			if strings.HasPrefix(value, "\"\"\"") {
				currentValue.WriteString(strings.TrimPrefix(value, "\"\"\""))
			} else {
				mf.Template = strings.Trim(value, "\"")
				currentCommand = ""
			}
			continue
		}

		if strings.HasPrefix(line, "PARAMETER ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "PARAMETER "), " ", 2)
			if len(parts) == 2 {
				mf.Parameters[parts[0]] = parseParameterValue(parts[1])
			}
			continue
		}

		if strings.HasPrefix(line, "ADAPTER ") {
			adapter := strings.TrimSpace(strings.TrimPrefix(line, "ADAPTER "))
			mf.Adapters = append(mf.Adapters, adapter)
			continue
		}

		if strings.HasPrefix(line, "LICENSE ") {
			currentCommand = "LICENSE"
			currentValue.Reset()
			value := strings.TrimPrefix(line, "LICENSE ")
			if strings.HasPrefix(value, "\"\"\"") {
				currentValue.WriteString(strings.TrimPrefix(value, "\"\"\""))
			} else {
				mf.License = strings.Trim(value, "\"")
				currentCommand = ""
			}
			continue
		}

		// Handle multiline content
		if currentCommand != "" {
			if strings.HasSuffix(line, "\"\"\"") {
				currentValue.WriteString("\n")
				currentValue.WriteString(strings.TrimSuffix(line, "\"\"\""))

				switch currentCommand {
				case "SYSTEM":
					mf.System = currentValue.String()
				case "TEMPLATE":
					mf.Template = currentValue.String()
				case "LICENSE":
					mf.License = currentValue.String()
				}
				currentCommand = ""
			} else {
				currentValue.WriteString("\n")
				currentValue.WriteString(line)
			}
		}
	}

	// Validate required fields
	if mf.From == "" {
		return nil, fmt.Errorf("Modelfile must specify FROM directive")
	}

	return mf, nil
}

// parseParameterValue converts string parameter values to appropriate types
func parseParameterValue(value string) interface{} {
	value = strings.TrimSpace(value)

	// Try boolean
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	// Try integer
	if i, err := fmt.Sscanf(value, "%d", new(int)); err == nil && i == 1 {
		var result int
		fmt.Sscanf(value, "%d", &result)
		return result
	}

	// Try float
	if i, err := fmt.Sscanf(value, "%f", new(float64)); err == nil && i == 1 {
		var result float64
		fmt.Sscanf(value, "%f", &result)
		return result
	}

	// Default to string
	return strings.Trim(value, "\"")
}

// SaveMLXModelfile saves an MLX model configuration to a Modelfile
func SaveMLXModelfile(path string, mf *MLXModelfile) error {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("FROM %s\n\n", mf.From))

	if mf.System != "" {
		content.WriteString("SYSTEM \"\"\"\n")
		content.WriteString(mf.System)
		content.WriteString("\n\"\"\"\n\n")
	}

	if mf.Template != "" {
		content.WriteString("TEMPLATE \"\"\"\n")
		content.WriteString(mf.Template)
		content.WriteString("\n\"\"\"\n\n")
	}

	for key, value := range mf.Parameters {
		content.WriteString(fmt.Sprintf("PARAMETER %s %v\n", key, value))
	}

	if len(mf.Adapters) > 0 {
		content.WriteString("\n")
		for _, adapter := range mf.Adapters {
			content.WriteString(fmt.Sprintf("ADAPTER %s\n", adapter))
		}
	}

	if mf.License != "" {
		content.WriteString("\nLICENSE \"\"\"\n")
		content.WriteString(mf.License)
		content.WriteString("\n\"\"\"\n")
	}

	return os.WriteFile(path, []byte(content.String()), 0644)
}

// CreateMLXModelFromModelfile creates an MLX model from a Modelfile
func CreateMLXModelFromModelfile(name string, modelfilePath string) error {
	mf, err := ParseMLXModelfile(modelfilePath)
	if err != nil {
		return fmt.Errorf("failed to parse Modelfile: %w", err)
	}

	// Ensure the model exists or can be downloaded
	manager := NewMLXModelManager()

	// If FROM is a HuggingFace path, download it
	if strings.Contains(mf.From, "/") && !filepath.IsAbs(mf.From) {
		if !manager.ModelExists(mf.From) {
			// Model will be downloaded on first use
			// For now, just validate it's a reasonable path
			if !strings.HasPrefix(mf.From, "mlx-community/") && !strings.Contains(mf.From, "mlx") {
				return fmt.Errorf("MLX model path should typically start with 'mlx-community/' or contain 'mlx'")
			}
		}
	}

	// Store the modelfile configuration
	configPath := filepath.Join(manager.GetModelsDir(), ".modelfiles", name+".json")
	os.MkdirAll(filepath.Dir(configPath), 0755)

	configData, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// GetMLXModelConfig retrieves the Modelfile configuration for an MLX model
func GetMLXModelConfig(name string) (*MLXModelfile, error) {
	manager := NewMLXModelManager()
	configPath := filepath.Join(manager.GetModelsDir(), ".modelfiles", name+".json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		// No custom config, return defaults
		return &MLXModelfile{
			From:       name,
			Parameters: make(map[string]interface{}),
		}, nil
	}

	var mf MLXModelfile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &mf, nil
}

// ConvertOptionsToMLXFormat converts Ollama API options to MLX-compatible format
func ConvertOptionsToMLXFormat(opts api.Options) map[string]interface{} {
	mlxOpts := make(map[string]interface{})

	// Map Ollama parameters to MLX equivalents
	if opts.Temperature > 0 {
		mlxOpts["temperature"] = opts.Temperature
	}
	if opts.TopK > 0 {
		mlxOpts["top_k"] = opts.TopK
	}
	if opts.TopP > 0 {
		mlxOpts["top_p"] = opts.TopP
	}
	if opts.NumPredict > 0 {
		mlxOpts["max_tokens"] = opts.NumPredict
	}
	if opts.RepeatPenalty > 0 {
		mlxOpts["repetition_penalty"] = opts.RepeatPenalty
	}
	if opts.Seed > 0 {
		mlxOpts["seed"] = opts.Seed
	}

	return mlxOpts
}
