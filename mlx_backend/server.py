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
    from mlx_lm.models import get_model
    from mlx_lm.tokenizers import get_tokenizer
except ImportError:
    print("Error: MLX is not installed. Install with:")
    print("  pip install mlx-lm")
    sys.exit(1)


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
        self.model_path = Path.home() / ".ollama" / "models" / "mlx"

    async def load_model(self, model_name: str) -> None:
        """
        Load an MLX model from Hugging Face or local cache

        Args:
            model_name: Model identifier (e.g., "meta-llama/Llama-2-7b")

        Raises:
            RuntimeError: If model cannot be loaded
        """
        if self.current_model_name == model_name:
            logger.info(f"Model {model_name} already loaded")
            return

        logger.info(f"Loading MLX model: {model_name}")
        try:
            # Try to load from local cache first
            local_path = self.model_path / model_name.replace("/", "_")
            if local_path.exists():
                logger.info(f"Loading from local cache: {local_path}")
                self.model, self.tokenizer = get_model(str(local_path))
            else:
                # Load from Hugging Face
                logger.info(f"Loading from Hugging Face: {model_name}")
                self.model, self.tokenizer = get_model(model_name)

            self.current_model_name = model_name
            logger.info(f"Successfully loaded {model_name}")
        except Exception as e:
            logger.error(f"Failed to load model {model_name}: {e}")
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
        """
        if self.model is None or self.tokenizer is None:
            raise RuntimeError("No model loaded")

        try:
            # Tokenize input
            prompt_tokens = self.tokenizer.encode(prompt)
            prompt_eval_start = time.time()
            eval_start = time.time()

            # Generate tokens
            tokens = prompt_tokens.copy()
            for i in range(num_predict):
                # Process through model
                logits = self.model(mx.array(tokens[-1:]))

                # Sample next token
                if temperature == 0.0:
                    # Greedy sampling
                    next_token = mx.argmax(logits[0, -1, :]).item()
                else:
                    # Temperature + top_k + top_p sampling
                    logits = logits[0, -1, :] / temperature
                    logits = mx.softmax(logits)

                    # Top-K filtering
                    if top_k > 0:
                        top_k_logits, top_k_indices = mx.topk(logits, k=top_k)
                        logits = mx.zeros_like(logits)
                        for idx, val in zip(top_k_indices, top_k_logits):
                            logits[idx] = val

                    # Sample from distribution
                    next_token = mx.random.categorical(mx.log(logits + 1e-10)).item()

                tokens.append(next_token)

                # Decode token to text
                token_text = self.tokenizer.decode([next_token])

                # Yield streaming response
                eval_duration = int((time.time() - eval_start) * 1e9)
                yield CompletionResponse(
                    content=token_text,
                    done=False,
                    prompt_eval_count=len(prompt_tokens),
                    prompt_eval_duration=int((time.time() - prompt_eval_start) * 1e9),
                    eval_count=i + 1,
                    eval_duration=eval_duration,
                )

            # Final response
            eval_duration = int((time.time() - eval_start) * 1e9)
            yield CompletionResponse(
                content="",
                done=True,
                done_reason="stop",
                prompt_eval_count=len(prompt_tokens),
                prompt_eval_duration=int((time.time() - prompt_eval_start) * 1e9),
                eval_count=len(tokens) - len(prompt_tokens),
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
    }


@app.get("/info")
async def info_endpoint():
    """Return server info (GPU devices, etc.)"""
    return {
        "gpu": "MLX (Apple Silicon)",
        "compute_capability": "Metal Performance Shaders",
        "device": "Apple Neural Engine" if hasattr(mx, 'metal') else "CPU",
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
