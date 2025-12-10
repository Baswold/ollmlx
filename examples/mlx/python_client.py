#!/usr/bin/env python3
"""
Example Python client for ollmlx using the Ollama Python library.

This demonstrates that ollmlx is 100% compatible with the official
Ollama Python client, but uses MLX models instead of GGUF.

Install: pip install ollama
"""

import ollama

def main():
    # MLX model from HuggingFace
    model_name = "mlx-community/Llama-3.2-1B-Instruct-4bit"

    print(f"🚀 ollmlx Python Client Example\n")

    # Pull the MLX model
    print(f"📥 Pulling MLX model: {model_name}")
    ollama.pull(model_name)
    print("✓ Model downloaded\n")

    # List all models (GGUF + MLX)
    print("📋 Available models:")
    models = ollama.list()
    for model in models['models']:
        format_type = model.get('details', {}).get('format', 'unknown')
        print(f"  - {model['name']} ({format_type})")
    print()

    # Generate text
    print(f"💬 Generating text with MLX model...")
    response = ollama.generate(
        model=model_name,
        prompt="Explain quantum computing in one sentence."
    )
    print(f"Response: {response['response']}\n")

    # Chat interface
    print("💬 Chat interface:")
    messages = [
        {
            'role': 'user',
            'content': 'Why is MLX faster on Apple Silicon?'
        }
    ]

    response = ollama.chat(model=model_name, messages=messages)
    print(f"Assistant: {response['message']['content']}\n")

    # Show model info
    print(f"ℹ️  Model information:")
    info = ollama.show(model_name)
    print(f"  Format: {info.get('details', {}).get('format', 'unknown')}")
    print(f"  Family: {info.get('details', {}).get('family', 'unknown')}")
    print(f"  Size: {info.get('size', 0) / (1024**3):.2f} GB")

    print("\n✨ Example complete!")
    print("\nollmlx is fully compatible with Ollama clients!")
    print("Use it with:")
    print("  - GitHub Copilot")
    print("  - VSCode extensions")
    print("  - LangChain")
    print("  - Any Ollama-compatible tool")

if __name__ == "__main__":
    main()
