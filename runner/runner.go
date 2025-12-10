package runner

import (
	"github.com/ollama/ollama/runner/llamarunner"
	"github.com/ollama/ollama/runner/mlxrunner"
	"github.com/ollama/ollama/runner/ollamarunner"
)

func Execute(args []string) error {
	if args[0] == "runner" {
		args = args[1:]
	}

	var newRunner bool
	var mlxRunner bool
	if args[0] == "--ollama-engine" {
		args = args[1:]
		newRunner = true
	} else if args[0] == "--mlx-engine" {
		args = args[1:]
		mlxRunner = true
	}

	if mlxRunner {
		return mlxrunner.Execute(args)
	} else if newRunner {
		return ollamarunner.Execute(args)
	} else {
		return llamarunner.Execute(args)
	}
}
