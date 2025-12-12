#!/usr/bin/env python3
"""
MLX Backend Server for ollmlx

This standalone Python server provides MLX-based model inference,
maintaining full API compatibility with Ollama's completion interface.

Architecture:
- Receives HTTP POST requests with CompletionRequest JSON
- Loads MLX models from Hugging Face or local cache
- Streams token-by-token responses in Ollama format
- Supports batching and concurrent requests via asyncio
"""

import asyncio
import json
import logging
import os
import signal
import sys
from dataclasses import dataclass, asdict
from pathlib import Path
from typing import AsyncIterator, Optional
import time

try:
    import uvicorn
    from fastapi import FastAPI, HTTPException
    from fastapi.responses import StreamingResponse
except ImportError:
    print("Error: FastAPI and uvicorn are required. Install with:")
    print("  pip install fastapi uvicorn")
    sys.exit(1)

try:
    import mlx.core as mx
    import mlx_lm
    from mlx_lm import load as get_model
    from mlx_lm.tokenizer_utils import TokenizerWrapper as get_tokenizer
    from mlx_lm.sample_utils import make_sampler
except ImportError:
    print("Error: MLX is not installed. Install with:")
    print("  pip install mlx-lm")
    sys.exit(1)

# Best-effort: prefer Metal GPU for acceleration
try:
    if hasattr(mx, "gpu"):
        mx.set_default_device(mx.gpu)  # type: ignore[attr-defined]
        logging.getLogger(__name__).info("Using Metal GPU via MLX")
except Exception as e:  # pragma: no cover - defensive
    logging.getLogger(__name__).warning("Could not set Metal device: %s", e)


logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


@dataclass
class ImageData:
    """Image data for multimodal models"""
    data: str  # Base64 encoded
    id: int


@dataclass
class Options:
    """Generation options"""
    temperature: float = 0.7
    top_k: int = 40
    top_p: float = 0.9
    num_predict: int = 128
    repeat_penalty: float = 1.1
    repeat_last_n: int = 64
    presence_penalty: float = 0.0
    frequency_penalty: float = 0.0


@dataclass
class CompletionRequest:
    """Request format from Ollama server"""
    prompt: str
    format: Optional[str] = None  # "json" or JSON schema
    images: Optional[list] = None
    options: Optional[dict] = None
    grammar: Optional[str] = None
    shift: bool = False
    truncate: bool = False
    logprobs: bool = False
    top_logprobs: int = 0


@dataclass
class CompletionResponse:
    """Streaming response format (compatible with Ollama)"""
    content: str = ""
    done_reason: str = ""
    done: bool = False
    prompt_eval_count: int = 0
    prompt_eval_duration: int = 0  # nanoseconds
    eval_count: int = 0
    eval_duration: int = 0  # nanoseconds
    logprobs: Optional[list] = None

    def to_json(self) -> str:
        """Convert to JSON string matching Ollama format"""
        return json.dumps(asdict(self))


class MLXModelManager:
    """Manages MLX model lifecycle"""

    def __init__(self):
        self.model = None
        self.tokenizer = None
        self.current_model_name = None
        default_model_path = Path.home() / ".ollama" / "models" / "mlx"
        env_model_path = os.environ.get("OLLAMA_MODELS", str(default_model_path))
        self.model_path = Path(env_model_path).expanduser()
        self.model_path.mkdir(parents=True, exist_ok=True)

        # Ensure Hugging Face downloads also use this cache location
        if not os.environ.get("HUGGINGFACE_HUB_CACHE"):
            os.environ["HUGGINGFACE_HUB_CACHE"] = str(self.model_path)

        if not os.environ.get("HF_HOME"):
            os.environ["HF_HOME"] = str(self.model_path)

        logger.info("Using MLX model path: %s", self.model_path)
        self.finetune_fn = None

    def _resolve_finetune(self):
        """Locate finetune entrypoint if available in mlx_lm."""
        if self.finetune_fn is not None:
            return self.finetune_fn

        # Try attribute on mlx_lm
        fn = getattr(mlx_lm, "finetune", None)
        if fn is None:
            # Try submodule import lazily
            try:
                import importlib

                mod = importlib.import_module("mlx_lm.finetune")
                fn = getattr(mod, "finetune", None)
            except Exception:
                fn = None

        self.finetune_fn = fn
        return fn

    async def load_model(self, model_name: str) -> None:
        """
        Load an MLX model from Hugging Face or local cache

        Args:
            model_name: Model identifier (e.g., "meta-llama/Llama-2-7b")

        Raises:
            ValueError: If model_name is invalid
            RuntimeError: If model cannot be loaded
            TimeoutError: If loading takes too long
        """
        # Input validation
        if not model_name or not isinstance(model_name, str):
            raise ValueError("Model name must be a non-empty string")

        if len(model_name) > 256:
            raise ValueError("Model name is too long (max 256 characters)")

        # Check if model is already loaded
        if self.current_model_name == model_name:
            logger.info(f"Model {model_name} already loaded")
            return

        logger.info(f"Loading MLX model: {model_name}")
        
        try:
            # Try to load from local cache first
            local_path = self.model_path / model_name.replace("/", "_")
            
            if local_path.exists():
                logger.info(f"Loading from local cache: {local_path}")
                
                # Validate that this is a proper MLX model directory
                config_path = local_path / "config.json"
                if not config_path.exists():
                    raise RuntimeError(f"Invalid MLX model directory: missing config.json in {local_path}")
                
                # Check for model weights
                safetensors_path = local_path / "model.safetensors"
                weights_path = local_path / "weights.npz"
                
                if not (safetensors_path.exists() or weights_path.exists()):
                    raise RuntimeError(f"Invalid MLX model directory: no model weights found in {local_path}")
                
                self.model, self.tokenizer = get_model(str(local_path))
            else:
                # Load from Hugging Face
                logger.info(f"Loading from Hugging Face: {model_name}")
                
                # Validate Hugging Face model format
                if "/" not in model_name:
                    logger.warning(f"Model name {model_name} doesn't look like a Hugging Face model ID (expected format: org/model)")
                
                self.model, self.tokenizer = get_model(model_name)

            # Validate that model and tokenizer were loaded
            if self.model is None:
                raise RuntimeError("Model loading returned None")
            
            if self.tokenizer is None:
                raise RuntimeError("Tokenizer loading returned None")

            self.current_model_name = model_name
            logger.info(f"Successfully loaded {model_name}")
            
        except Exception as e:
            logger.error(f"Failed to load model {model_name}: {e}")
            
            # Clean up if we partially loaded
            self.model = None
            self.tokenizer = None
            self.current_model_name = None
            
            # Provide more specific error messages
            error_msg = str(e)
            if "not found" in error_msg.lower() or "404" in error_msg:
                raise RuntimeError(f"Model not found: {model_name}. Please check the model name and try again.")
            elif "connection" in error_msg.lower() or "network" in error_msg.lower():
                raise RuntimeError(f"Network error while loading model: {e}. Please check your internet connection.")
            elif "memory" in error_msg.lower() or "out of memory" in error_msg.lower():
                raise RuntimeError(f"Out of memory while loading model: {e}. Try a smaller model or free up system resources.")
            else:
                raise RuntimeError(f"Failed to load model: {e}")

    async def generate(
        self,
        prompt: str,
        temperature: float = 0.7,
        top_k: int = 40,
        top_p: float = 0.9,
        num_predict: int = 128,
        repeat_penalty: float = 1.1,
    ) -> AsyncIterator[CompletionResponse]:
        """
        Generate tokens using MLX, yielding streaming responses

        Args:
            prompt: Input text
            temperature: Sampling temperature (0.0 = deterministic)
            top_k: Top-K sampling
            top_p: Nucleus sampling
            num_predict: Maximum tokens to generate
            repeat_penalty: Penalty for repeating tokens

        Yields:
            CompletionResponse objects for streaming

        Raises:
            ValueError: If prompt is invalid or parameters are out of range
            RuntimeError: If generation fails
        """
        # Input validation
        if not prompt or not isinstance(prompt, str):
            raise ValueError("Prompt must be a non-empty string")

        if len(prompt) > 8192:  # Reasonable max prompt length
            raise ValueError("Prompt is too long (max 8192 characters)")

        # Validate sampling parameters
        if not (0.0 <= temperature <= 2.0):
            raise ValueError("Temperature must be between 0.0 and 2.0")

        if not (0 <= top_k <= 1000):
            raise ValueError("Top-K must be between 0 and 1000")

        if not (0.0 <= top_p <= 1.0):
            raise ValueError("Top-P must be between 0.0 and 1.0")

        if not (1 <= num_predict <= 4096):
            raise ValueError("Number of tokens to predict must be between 1 and 4096")

        if self.model is None or self.tokenizer is None:
            raise RuntimeError("No model loaded")

        try:
            # Tokenize input with error handling
            try:
                prompt_tokens = self.tokenizer.encode(prompt)
                if len(prompt_tokens) > 8192:  # Max context length
                    raise ValueError(f"Prompt exceeds maximum context length (8192 tokens, got {len(prompt_tokens)})")
            except Exception as tokenize_error:
                raise RuntimeError(f"Failed to tokenize prompt: {tokenize_error}")

            prompt_eval_start = time.time()
            eval_start = time.time()

            # Create sampler for MLX generation with validation
            try:
                sampler = make_sampler(
                    temp=temperature,
                    top_p=top_p,
                    top_k=top_k
                )
            except Exception as sampler_error:
                raise RuntimeError(f"Failed to create sampler: {sampler_error}")

            # Use mlx_lm's stream_generate for proper token generation
            generated_tokens = []
            token_count = 0
            max_tokens_generated = 0
            
            # Generate tokens using mlx_lm's streaming interface
            try:
                for response in mlx_lm.stream_generate(
                    self.model, 
                    self.tokenizer, 
                    prompt=prompt,
                    max_tokens=num_predict,
                    sampler=sampler
                ):
                    if hasattr(response, 'text'):
                        token_text = response.text
                        generated_tokens.append(token_text)
                        token_count += 1
                        max_tokens_generated += 1
                        
                        # Safety check: prevent infinite loops
                        if max_tokens_generated > num_predict * 2:
                            logger.warning(f"Generated more tokens than requested ({max_tokens_generated} > {num_predict})")
                            break
                        
                        # Yield streaming response
                        eval_duration = int((time.time() - eval_start) * 1e9)
                        yield CompletionResponse(
                            content=token_text,
                            done=False,
                            prompt_eval_count=len(prompt_tokens),
                            prompt_eval_duration=int((time.time() - prompt_eval_start) * 1e9),
                            eval_count=token_count,
                            eval_duration=eval_duration,
                        )
                        
                        # Reset timer for next token
                        eval_start = time.time()
                        
                        # Stop if we've generated enough tokens
                        if token_count >= num_predict:
                            break

            except Exception as generate_error:
                error_msg = str(generate_error)
                logger.error(f"Generation failed: {error_msg}")
                
                # Provide specific error messages
                if "out of memory" in error_msg.lower():
                    yield CompletionResponse(
                        content="Error: Out of memory during generation. Try a smaller model or shorter prompt.",
                        done=True,
                        done_reason="error",
                    )
                elif "timeout" in error_msg.lower():
                    yield CompletionResponse(
                        content="Error: Generation timed out. Try a shorter prompt or fewer tokens.",
                        done=True,
                        done_reason="error",
                    )
                else:
                    yield CompletionResponse(
                        content=f"Error: Generation failed - {error_msg}",
                        done=True,
                        done_reason="error",
                    )
                return

            # Final response
            eval_duration = int((time.time() - eval_start) * 1e9)
            yield CompletionResponse(
                content="",
                done=True,
                done_reason="stop",
                prompt_eval_count=len(prompt_tokens),
                prompt_eval_duration=int((time.time() - prompt_eval_start) * 1e9),
                eval_count=token_count,
                eval_duration=eval_duration,
            )

        except Exception as e:
            logger.error(f"Generation failed: {e}")
            yield CompletionResponse(
                content=f"Error: {e}",
                done=True,
                done_reason="error",
            )


# Global model manager
model_manager = MLXModelManager()
app = FastAPI(title="MLX Backend Server")


@app.post("/completion")
async def completion_endpoint(request: dict) -> StreamingResponse:
    """
    Handle completion requests from Ollama server

    Matches the interface of llama.cpp runner's /completion endpoint
    """
    try:
        # Parse request
        req = CompletionRequest(**request)

        if not req.prompt:
            raise HTTPException(status_code=400, detail="Empty prompt")

        # Parse options
        options = Options(**(req.options or {}))

        # Generate responses
        async def response_generator():
            async for response in model_manager.generate(
                prompt=req.prompt,
                temperature=options.temperature,
                top_k=options.top_k,
                top_p=options.top_p,
                num_predict=options.num_predict,
                repeat_penalty=options.repeat_penalty,
            ):
                # Emit in SSE format with line ending
                yield response.to_json() + "\n"

        return StreamingResponse(
            response_generator(),
            media_type="application/json",
        )

    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Completion error: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/load")
async def load_endpoint(request: dict):
    """
    Handle model loading requests

    Matches the interface of llama.cpp runner's /load endpoint
    """
    try:
        model_name = request.get("model")
        if not model_name:
            raise HTTPException(status_code=400, detail="Missing model name")

        await model_manager.load_model(model_name)

        return {
            "status": "loaded",
            "model": model_name,
            "parameters": {}  # Model parameters would go here
        }
    except Exception as e:
        logger.error(f"Load error: {e}")
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/health")
async def health_check():
    """Health check endpoint"""
    return {
        "status": "ok",
        "model_loaded": model_manager.current_model_name is not None,
        "current_model": model_manager.current_model_name,
        "device": str(mx.default_device()),
    }


@app.get("/info")
async def info_endpoint():
    """Return server info (GPU devices, etc.)"""
    return {
        "gpu": "MLX (Apple Silicon)",
        "compute_capability": "Metal Performance Shaders",
        "device": "Apple Neural Engine" if hasattr(mx, 'metal') else "CPU",
    }


@app.post("/finetune")
async def finetune_endpoint(request: dict):
    """Run a best-effort fine-tune using mlx_lm if available."""
    model_name = request.get("model")
    dataset = request.get("dataset")
    output_dir = Path(request.get("output_dir", "./finetuned"))
    epochs = int(request.get("epochs", 1))
    batch_size = int(request.get("batch_size", 1))
    learning_rate = float(request.get("learning_rate", 1e-4))

    if not model_name or not dataset:
        raise HTTPException(status_code=400, detail="Missing required fields: model, dataset")

    fn = model_manager._resolve_finetune()
    if fn is None:
        raise HTTPException(status_code=501, detail="MLX fine-tuning entrypoint not available in this build (mlx_lm missing finetune)")

    output_dir.mkdir(parents=True, exist_ok=True)

    def run_ft_sync():
        # Attempt a flexible call signature; fall back if unsupported
        try:
            fn(
                model=model_name,
                data=dataset,
                output_dir=str(output_dir),
                epochs=epochs,
                batch_size=batch_size,
                learning_rate=learning_rate,
            )
        except TypeError:
            fn(model_name, dataset, str(output_dir))

    try:
        await asyncio.to_thread(run_ft_sync)
    except Exception as e:  # pragma: no cover - runtime safeguard
        raise HTTPException(status_code=500, detail=f"Fine-tune failed: {e}")

    return {
        "status": "completed",
        "model": model_name,
        "output_dir": str(output_dir),
        "epochs": epochs,
        "batch_size": batch_size,
        "learning_rate": learning_rate,
    }


def main():
    """Start the MLX backend server"""
    import argparse

    parser = argparse.ArgumentParser(description="MLX Backend Server for ollmlx")
    parser.add_argument("--port", type=int, default=8000, help="Port to listen on")
    parser.add_argument("--host", default="127.0.0.1", help="Host to bind to")
    parser.add_argument("--workers", type=int, default=1, help="Number of worker processes")
    parser.add_argument("--log-level", default="info", help="Log level")

    args = parser.parse_args()

    logger.info(f"Starting MLX Backend Server on {args.host}:{args.port}")

    # Run uvicorn server
    uvicorn.run(
        app,
        host=args.host,
        port=args.port,
        workers=args.workers,
        log_level=args.log_level,
    )


if __name__ == "__main__":
    main()
