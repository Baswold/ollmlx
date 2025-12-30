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
import dataclasses
from dataclasses import dataclass, asdict, fields
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

# Vision model support
try:
    import mlx_vlm
    from mlx_vlm import load as load_vlm
    from mlx_vlm.prompt_utils import apply_chat_template
    from mlx_vlm.utils import load_config
    MLX_VLM_AVAILABLE = True
except ImportError:
    MLX_VLM_AVAILABLE = False
    mlx_vlm = None

import base64
from io import BytesIO
try:
    from PIL import Image
    PIL_AVAILABLE = True
except ImportError:
    PIL_AVAILABLE = False
    Image = None

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

# Suppress noisy asyncio warnings when clients disconnect mid-stream
logging.getLogger("asyncio").setLevel(logging.ERROR)


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
    num_predict: int = 16384  # Default to 16K tokens for reasoning models
    repeat_penalty: float = 1.1
    repeat_last_n: int = 64
    presence_penalty: float = 0.0
    frequency_penalty: float = 0.0
    # Additional Ollama-compatible options (accepted but some may be ignored)
    num_ctx: int = 32768  # Context window size
    num_batch: int = 512  # Batch size for prompt processing
    num_gpu: int = -1  # Number of GPU layers (-1 = all)
    main_gpu: int = 0  # Main GPU index
    low_vram: bool = False  # Low VRAM mode
    seed: int = -1  # Random seed (-1 = random)
    stop: Optional[list] = None  # Stop sequences
    tfs_z: float = 1.0  # Tail free sampling
    typical_p: float = 1.0  # Typical p sampling
    mirostat: int = 0  # Mirostat sampling mode
    mirostat_tau: float = 5.0  # Mirostat target entropy
    mirostat_eta: float = 0.1  # Mirostat learning rate


@dataclass
class CompletionRequest:
    """Request format from Ollama server"""
    prompt: str
    model: Optional[str] = None  # Model name (passed through from Ollama)
    format: Optional[str] = None  # "json" or JSON schema
    images: Optional[list] = None
    options: Optional[dict] = None
    grammar: Optional[str] = None
    shift: bool = False
    truncate: bool = False
    logprobs: bool = False
    top_logprobs: int = 0
    tools: Optional[list] = None
    stream: Optional[bool] = None  # Whether to stream responses
    keep_alive: Optional[str] = None  # Keep-alive duration
    # Additional fields from Ollama GenerateRequest that we accept but don't use
    suffix: Optional[str] = None
    system: Optional[str] = None
    template: Optional[str] = None
    context: Optional[list] = None
    raw: bool = False
    think: Optional[bool] = None


def parse_completion_request(data: dict) -> CompletionRequest:
    """Parse a completion request, ignoring unknown fields."""
    import dataclasses
    known_fields = {f.name for f in dataclasses.fields(CompletionRequest)}
    filtered = {k: v for k, v in data.items() if k in known_fields}
    return CompletionRequest(**filtered)


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
    tool_calls: Optional[list] = None

    def to_json(self) -> str:
        """Convert to JSON string matching Ollama format"""
        return json.dumps(asdict(self))


def parse_tool_calls(text: str) -> Optional[list]:
    """Best-effort extraction of tool_calls JSON payload from model output.

    Handles multiple formats that models might produce:
    1. OpenAI-style: {"tool_calls":[{"function":{"name":"fn","arguments":{...}}}]}
    2. Simple array: [{"name":"fn","arguments":{...}}]
    3. Inline function call: {"name":"fn","arguments":{...}}
    4. Tool name as key: {"get_weather":{"location":"SF"}}
    """
    text = text.strip()

    # Try to find JSON in the text (models often add extra text)
    json_candidates = []

    # Look for JSON objects/arrays in the text
    depth = 0
    start = -1
    for i, c in enumerate(text):
        if c in '{[':
            if depth == 0:
                start = i
            depth += 1
        elif c in '}]':
            depth -= 1
            if depth == 0 and start >= 0:
                json_candidates.append(text[start:i+1])
                start = -1

    # If no candidates found, try the whole text
    if not json_candidates:
        json_candidates = [text]

    for candidate in json_candidates:
        try:
            data = json.loads(candidate)
        except Exception:
            continue

        normalized = []

        # Format 1: {"tool_calls": [...]}
        if isinstance(data, dict) and "tool_calls" in data:
            calls = data["tool_calls"]
            if isinstance(calls, list):
                for idx, call in enumerate(calls):
                    parsed = _parse_single_tool_call(call, idx)
                    if parsed:
                        normalized.append(parsed)

        # Format 2: Direct array of calls
        elif isinstance(data, list):
            for idx, call in enumerate(data):
                parsed = _parse_single_tool_call(call, idx)
                if parsed:
                    normalized.append(parsed)

        # Format 3: Single call object
        elif isinstance(data, dict):
            parsed = _parse_single_tool_call(data, 0)
            if parsed:
                normalized.append(parsed)

        if normalized:
            return normalized

    return None


def _parse_single_tool_call(call: any, idx: int) -> Optional[dict]:
    """Parse a single tool call from various formats."""
    if not isinstance(call, dict):
        return None

    name = None
    arguments = {}

    # Format A: {"function": {"name": "fn", "arguments": {...}}}
    if "function" in call:
        func = call["function"]
        if isinstance(func, dict):
            name = func.get("name")
            arguments = func.get("arguments", {})

    # Format B: {"name": "fn", "arguments": {...}}
    elif "name" in call:
        name = call.get("name")
        arguments = call.get("arguments", {})

    # Format C: {"tool_name": {...}} (tool name as key)
    elif len(call) == 1:
        key = list(call.keys())[0]
        if isinstance(call[key], dict):
            name = key
            arguments = call[key]

    if not name:
        return None

    # Ensure arguments is a dict
    if isinstance(arguments, str):
        try:
            arguments = json.loads(arguments)
        except Exception:
            arguments = {"raw": arguments}
    elif not isinstance(arguments, dict):
        arguments = {}

    return {
        "id": call.get("id") or f"call_{idx}",
        "function": {
            "index": idx,
            "name": name,
            "arguments": arguments,
        },
    }


class MLXModelManager:
    """Manages MLX model lifecycle"""

    def __init__(self):
        self.model = None
        self.tokenizer = None
        self.processor = None  # For vision models
        self.image_processor = None  # For vision models
        self.config = None  # Model config for vision models
        self.current_model_name = None
        self.is_vision_model = False
        # Respect OLLAMA_MODELS environment variable like the Go code does
        # Go code uses ~/.ollmlx/models as default, not ~/.ollama/models
        default_base_path = Path.home() / ".ollmlx" / "models"
        env_base_path = os.environ.get("OLLAMA_MODELS")
        if env_base_path:
            base_path = Path(env_base_path).expanduser()
        else:
            base_path = default_base_path
        # Models are stored directly in the base path, not in an 'mlx' subdirectory
        self.model_path = base_path
        self.model_path.mkdir(parents=True, exist_ok=True)

        # Ensure Hugging Face downloads also use this cache location
        if not os.environ.get("HUGGINGFACE_HUB_CACHE"):
            os.environ["HUGGINGFACE_HUB_CACHE"] = str(self.model_path)

        if not os.environ.get("HF_HOME"):
            os.environ["HF_HOME"] = str(self.model_path)

        logger.info("Using MLX model path: %s", self.model_path)

    def _is_vision_model(self, model_path: str) -> bool:
        """Check if the model is a vision-language model based on config."""
        try:
            config_path = Path(model_path) / "config.json"
            if config_path.exists():
                import json
                with open(config_path) as f:
                    config = json.load(f)
                # Check for vision model indicators
                vision_indicators = [
                    "vision_config",
                    "vision_tower",
                    "image_processor",
                    "vision_encoder",
                    "visual",
                    "image_tower",
                ]
                config_str = json.dumps(config).lower()
                for indicator in vision_indicators:
                    if indicator in config_str:
                        return True
                # Check model_type for known VLM architectures
                model_type = config.get("model_type", "").lower()
                vlm_types = ["llava", "pixtral", "qwen2_vl", "idefics", "paligemma", "molmo"]
                if any(vt in model_type for vt in vlm_types):
                    return True
        except Exception as e:
            logger.debug(f"Could not check vision model config: {e}")
        return False

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
            model_source = str(local_path) if local_path.exists() else model_name

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
            else:
                logger.info(f"Loading from Hugging Face: {model_name}")
                if "/" not in model_name:
                    logger.warning(f"Model name {model_name} doesn't look like a Hugging Face model ID (expected format: org/model)")

            # Check if this is a vision model
            is_vlm = self._is_vision_model(model_source)

            if is_vlm and MLX_VLM_AVAILABLE:
                logger.info(f"Detected vision-language model, loading with mlx-vlm")
                self.model, self.processor = load_vlm(model_source)
                self.tokenizer = self.processor
                self.config = load_config(model_source)
                self.is_vision_model = True
                self.image_processor = self.processor
            else:
                if is_vlm and not MLX_VLM_AVAILABLE:
                    logger.warning("Vision model detected but mlx-vlm not installed. Install with: pip install mlx-vlm")
                # Use lazy=True for faster initial load (weights load on first use)
                self.model, self.tokenizer = get_model(model_source, lazy=True)
                self.is_vision_model = False
                self.processor = None
                self.image_processor = None
                self.config = None

            # Validate that model and tokenizer were loaded
            if self.model is None:
                raise RuntimeError("Model loading returned None")

            if self.tokenizer is None:
                raise RuntimeError("Tokenizer loading returned None")

            self.current_model_name = model_name
            logger.info(f"Successfully loaded {model_name} (vision={self.is_vision_model})")

        except Exception as e:
            logger.error(f"Failed to load model {model_name}: {e}")

            # Clean up if we partially loaded
            self.model = None
            self.tokenizer = None
            self.processor = None
            self.image_processor = None
            self.config = None
            self.current_model_name = None
            self.is_vision_model = False

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

    def _decode_images(self, images: Optional[list]) -> list:
        """Decode base64 images to PIL Image objects."""
        if not images or not PIL_AVAILABLE:
            return []

        decoded = []
        for img_data in images:
            try:
                # Handle different image formats
                if isinstance(img_data, dict):
                    # Format: {"data": "base64...", "id": 0}
                    b64_data = img_data.get("data", "")
                elif isinstance(img_data, str):
                    # Direct base64 string
                    b64_data = img_data
                else:
                    # Raw bytes
                    b64_data = base64.b64encode(img_data).decode("utf-8")

                # Remove data URL prefix if present
                if b64_data.startswith("data:"):
                    b64_data = b64_data.split(",", 1)[-1]

                # Decode base64 and create PIL Image
                img_bytes = base64.b64decode(b64_data)
                img = Image.open(BytesIO(img_bytes))
                # Convert to RGB if necessary (for models that expect RGB)
                if img.mode != "RGB":
                    img = img.convert("RGB")
                decoded.append(img)
            except Exception as e:
                logger.warning(f"Failed to decode image: {e}")
                continue

        return decoded

    async def generate(
        self,
        prompt: str,
        temperature: float = 0.7,
        top_k: int = 40,
        top_p: float = 0.9,
        num_predict: int = 16384,
        repeat_penalty: float = 1.1,
        images: Optional[list] = None,
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
            images: Optional list of base64-encoded images for vision models

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

        if not (1 <= num_predict <= 131072):
            raise ValueError("Number of tokens to predict must be between 1 and 131072")

        if self.model is None or self.tokenizer is None:
            raise RuntimeError("No model loaded")

        # Decode images if provided
        decoded_images = self._decode_images(images) if images else []

        # Check if images were provided but model doesn't support them
        if decoded_images and not self.is_vision_model:
            logger.warning("Images provided but model is not a vision model. Images will be ignored.")
            decoded_images = []

        try:
            prompt_eval_start = time.time()
            eval_start = time.time()

            # Handle vision model generation
            if self.is_vision_model and decoded_images and MLX_VLM_AVAILABLE:
                async for response in self._generate_with_vision(
                    prompt=prompt,
                    images=decoded_images,
                    temperature=temperature,
                    top_k=top_k,
                    top_p=top_p,
                    num_predict=num_predict,
                    prompt_eval_start=prompt_eval_start,
                ):
                    yield response
                return

            # Standard text generation
            # Tokenize input with error handling
            try:
                prompt_tokens = self.tokenizer.encode(prompt)
                if len(prompt_tokens) > 131072:  # Max context length (128K for MLX)
                    raise ValueError(f"Prompt exceeds maximum context length (131072 tokens, got {len(prompt_tokens)})")
            except Exception as tokenize_error:
                raise RuntimeError(f"Failed to tokenize prompt: {tokenize_error}")

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

    async def _generate_with_vision(
        self,
        prompt: str,
        images: list,
        temperature: float,
        top_k: int,
        top_p: float,
        num_predict: int,
        prompt_eval_start: float,
    ) -> AsyncIterator[CompletionResponse]:
        """
        Generate tokens using a vision-language model with images.

        Args:
            prompt: Text prompt
            images: List of PIL Image objects
            temperature: Sampling temperature
            top_k: Top-K sampling
            top_p: Nucleus sampling
            num_predict: Max tokens to generate
            prompt_eval_start: Start time for prompt evaluation

        Yields:
            CompletionResponse objects for streaming
        """
        if not MLX_VLM_AVAILABLE:
            yield CompletionResponse(
                content="Error: mlx-vlm is not installed. Install with: pip install mlx-vlm",
                done=True,
                done_reason="error",
            )
            return

        try:
            from mlx_vlm import generate as vlm_generate

            eval_start = time.time()
            token_count = 0

            # Format the prompt with image placeholders for the specific model
            # Most VLMs expect <image> tokens in the prompt
            formatted_prompt = prompt
            if "<image>" not in prompt.lower():
                # Add image placeholder if not present
                formatted_prompt = "<image>\n" + prompt

            # Apply chat template if available
            if hasattr(self.processor, 'apply_chat_template'):
                messages = [{"role": "user", "content": formatted_prompt}]
                try:
                    formatted_prompt = self.processor.apply_chat_template(
                        messages,
                        tokenize=False,
                        add_generation_prompt=True
                    )
                except Exception as e:
                    logger.debug(f"Could not apply chat template: {e}")

            # Use the first image (most models support single image)
            image = images[0] if images else None

            try:
                # Try streaming generation with mlx-vlm
                # Different versions of mlx-vlm have different APIs
                if hasattr(mlx_vlm, 'stream_generate'):
                    for response in mlx_vlm.stream_generate(
                        self.model,
                        self.processor,
                        formatted_prompt,
                        image,
                        max_tokens=num_predict,
                        temp=temperature,
                    ):
                        if hasattr(response, 'text'):
                            token_text = response.text
                        else:
                            token_text = str(response)

                        token_count += 1
                        eval_duration = int((time.time() - eval_start) * 1e9)

                        yield CompletionResponse(
                            content=token_text,
                            done=False,
                            prompt_eval_count=0,
                            prompt_eval_duration=int((time.time() - prompt_eval_start) * 1e9),
                            eval_count=token_count,
                            eval_duration=eval_duration,
                        )
                        eval_start = time.time()

                        if token_count >= num_predict:
                            break
                else:
                    # Fallback to non-streaming generation
                    output = vlm_generate(
                        self.model,
                        self.processor,
                        formatted_prompt,
                        image,
                        max_tokens=num_predict,
                        temp=temperature,
                        verbose=False,
                    )

                    # Yield the full output
                    token_count = len(output.split()) if output else 0
                    eval_duration = int((time.time() - eval_start) * 1e9)

                    yield CompletionResponse(
                        content=output,
                        done=False,
                        prompt_eval_count=0,
                        prompt_eval_duration=int((time.time() - prompt_eval_start) * 1e9),
                        eval_count=token_count,
                        eval_duration=eval_duration,
                    )

            except Exception as gen_error:
                logger.error(f"Vision generation failed: {gen_error}")
                yield CompletionResponse(
                    content=f"Error: Vision generation failed - {gen_error}",
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
                prompt_eval_count=0,
                prompt_eval_duration=int((time.time() - prompt_eval_start) * 1e9),
                eval_count=token_count,
                eval_duration=eval_duration,
            )

        except Exception as e:
            logger.error(f"Vision generation failed: {e}")
            yield CompletionResponse(
                content=f"Error: {e}",
                done=True,
                done_reason="error",
            )

    def _detect_embedding_strategy(self) -> str:
        """
        Detect the best embedding extraction strategy based on model architecture.

        Returns:
            Strategy name: 'cls', 'last_token', 'mean', or 'mean_no_special'
        """
        model_name = (self.current_model_name or "").lower()

        # BERT-like models use [CLS] token (first token)
        if any(name in model_name for name in ["bert", "roberta", "distilbert", "electra"]):
            return "cls"

        # GPT-like models often use last token
        if any(name in model_name for name in ["gpt", "bloom", "opt"]):
            return "last_token"

        # E5, BGE, and GTE models use CLS token
        if any(name in model_name for name in ["e5", "bge", "gte"]):
            return "cls"

        # Sentence transformers and most others use mean pooling (excluding special tokens)
        if any(name in model_name for name in ["sentence", "instructor", "minilm"]):
            return "mean_no_special"

        # Default to mean pooling (excluding padding tokens where possible)
        return "mean_no_special"

    def _get_special_token_ids(self) -> set:
        """Get the set of special token IDs to exclude from mean pooling."""
        special_ids = set()

        if hasattr(self.tokenizer, 'pad_token_id') and self.tokenizer.pad_token_id is not None:
            special_ids.add(self.tokenizer.pad_token_id)
        if hasattr(self.tokenizer, 'cls_token_id') and self.tokenizer.cls_token_id is not None:
            special_ids.add(self.tokenizer.cls_token_id)
        if hasattr(self.tokenizer, 'sep_token_id') and self.tokenizer.sep_token_id is not None:
            special_ids.add(self.tokenizer.sep_token_id)
        if hasattr(self.tokenizer, 'bos_token_id') and self.tokenizer.bos_token_id is not None:
            special_ids.add(self.tokenizer.bos_token_id)
        if hasattr(self.tokenizer, 'eos_token_id') and self.tokenizer.eos_token_id is not None:
            special_ids.add(self.tokenizer.eos_token_id)
        if hasattr(self.tokenizer, 'unk_token_id') and self.tokenizer.unk_token_id is not None:
            special_ids.add(self.tokenizer.unk_token_id)

        return special_ids

    def embed(self, text: str) -> list[float]:
        """
        Generate embeddings for the given text using model-aware extraction.

        Supports multiple strategies:
        - 'cls': Use [CLS] token embedding (BERT-like models)
        - 'last_token': Use last token embedding (GPT-like models)
        - 'mean': Mean pooling over all tokens
        - 'mean_no_special': Mean pooling excluding special tokens (recommended)

        Args:
            text: Input text to embed

        Returns:
            List of floats representing the embedding vector

        Raises:
            RuntimeError: If no model is loaded or embedding generation fails
        """
        if self.model is None or self.tokenizer is None:
            raise RuntimeError("No model loaded")

        if not text or not isinstance(text, str):
            raise ValueError("Text must be a non-empty string")

        try:
            strategy = self._detect_embedding_strategy()
            logger.debug(f"Using embedding strategy: {strategy}")

            # Tokenize the input
            tokens = self.tokenizer.encode(text)
            token_list = tokens if isinstance(tokens, list) else tokens.tolist()

            if isinstance(tokens, list):
                tokens = mx.array([tokens])
            elif len(tokens.shape) == 1:
                tokens = tokens.reshape(1, -1)

            # Get hidden states from the model
            # Try different model architectures
            embeddings = None

            if hasattr(self.model, 'model') and hasattr(self.model.model, 'embed_tokens'):
                # Get token embeddings from embed layer
                token_emb = self.model.model.embed_tokens(tokens)

                # If model has transformer layers, get full hidden states
                if hasattr(self.model, '__call__'):
                    try:
                        outputs = self.model(tokens)
                        if hasattr(outputs, 'last_hidden_state'):
                            embeddings = outputs.last_hidden_state
                        elif isinstance(outputs, tuple) and len(outputs) > 0:
                            embeddings = outputs[0]
                        else:
                            embeddings = outputs
                    except Exception:
                        embeddings = token_emb
                else:
                    embeddings = token_emb

            elif hasattr(self.model, 'embed_tokens'):
                embeddings = self.model.embed_tokens(tokens)
            else:
                # Fallback: run forward pass
                outputs = self.model(tokens)
                if hasattr(outputs, 'last_hidden_state'):
                    embeddings = outputs.last_hidden_state
                elif isinstance(outputs, tuple):
                    embeddings = outputs[0]
                else:
                    embeddings = outputs

            # Apply strategy-specific pooling
            # embeddings shape: [batch, seq_len, hidden_dim]
            if strategy == "cls":
                # Use first token ([CLS] or equivalent)
                embedding = embeddings[:, 0, :]

            elif strategy == "last_token":
                # Use last token (for GPT-like models)
                embedding = embeddings[:, -1, :]

            elif strategy == "mean_no_special":
                # Mean pooling excluding special tokens
                special_ids = self._get_special_token_ids()

                if special_ids:
                    # Create mask for non-special tokens
                    mask = mx.ones((1, len(token_list)), dtype=mx.float32)
                    for i, tok_id in enumerate(token_list):
                        if tok_id in special_ids:
                            mask = mask.at[0, i].set(0.0)

                    # Expand mask for hidden dimension
                    mask = mx.expand_dims(mask, axis=-1)

                    # Masked mean
                    masked_emb = embeddings * mask
                    sum_mask = mx.sum(mask, axis=1, keepdims=True)
                    sum_mask = mx.maximum(sum_mask, mx.array(1e-9))  # Avoid division by zero
                    embedding = mx.sum(masked_emb, axis=1) / sum_mask.squeeze(-1)
                else:
                    # Fallback to simple mean if no special tokens detected
                    embedding = mx.mean(embeddings, axis=1)

            else:  # strategy == "mean"
                # Simple mean pooling over all tokens
                embedding = mx.mean(embeddings, axis=1)

            # Normalize the embedding (L2 normalization)
            norm = mx.sqrt(mx.sum(embedding * embedding, axis=-1, keepdims=True))
            embedding = embedding / mx.maximum(norm, mx.array(1e-12))

            # Convert to list of floats
            result = embedding[0].tolist()

            logger.debug(f"Generated embedding with dimension {len(result)} using strategy '{strategy}'")
            return result

        except Exception as e:
            logger.error(f"Embedding generation failed: {e}")
            raise RuntimeError(f"Failed to generate embedding: {e}")

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
        # Parse request, ignoring unknown fields from Ollama
        req = parse_completion_request(request)
        tools = request.get("tools") or []
        tools_present = bool(tools)
        images = req.images or []

        # Empty prompt = model preload/warmup request, just return done
        if not req.prompt:
            async def empty_response():
                yield CompletionResponse(content="", done=True, done_reason="stop").to_json() + "\n"
            return StreamingResponse(empty_response(), media_type="application/x-ndjson")

        # Parse options, filtering out unknown fields
        known_option_fields = {f.name for f in fields(Options)}
        filtered_options = {k: v for k, v in (req.options or {}).items() if k in known_option_fields}
        options = Options(**filtered_options)

        # Generate responses
        async def response_generator():
            # Check if this is a vision model request (images present and vision model loaded)
            has_images = bool(images) and model_manager.is_vision_model

            if not tools_present:
                async for response in model_manager.generate(
                    prompt=req.prompt,
                    temperature=options.temperature,
                    top_k=options.top_k,
                    top_p=options.top_p,
                    num_predict=options.num_predict,
                    repeat_penalty=options.repeat_penalty,
                    images=images,
                ):
                    # Emit in SSE format with line ending
                    yield response.to_json() + "\n"
                return

            # For tool-calling, accumulate the full response and try to extract tool_calls.
            # FIX: Use vision-aware generation for tool calling with images
            collected = []
            last_chunk: Optional[CompletionResponse] = None

            async for response in model_manager.generate(
                prompt=req.prompt,
                temperature=options.temperature,
                top_k=options.top_k,
                top_p=options.top_p,
                num_predict=options.num_predict,
                repeat_penalty=options.repeat_penalty,
                images=images,  # generate() now properly routes to vision if needed
            ):
                collected.append(response.content)
                last_chunk = response

            combined = "".join(collected).strip()
            tool_calls = parse_tool_calls(combined)

            # Build a final chunk that carries tool_calls if we found any.
            final = last_chunk or CompletionResponse()
            # FIX: Preserve the original content even if tool calls are found
            # This allows clients to see the model's reasoning alongside tool calls
            final.content = combined
            final.done = True
            final.done_reason = "tool_calls" if tool_calls else "stop"
            final.tool_calls = tool_calls
            yield final.to_json() + "\n"

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
    capabilities = ["completion"]
    if model_manager.is_vision_model:
        capabilities.append("vision")
    return {
        "status": "ok",
        "model_loaded": model_manager.current_model_name is not None,
        "current_model": model_manager.current_model_name,
        "device": str(mx.default_device()),
        "is_vision_model": model_manager.is_vision_model,
        "capabilities": capabilities,
        "mlx_vlm_available": MLX_VLM_AVAILABLE,
        "pil_available": PIL_AVAILABLE,
    }


@app.get("/info")
async def info_endpoint():
    """Return server info (GPU devices, etc.)"""
    return {
        "gpu": "MLX (Apple Silicon)",
        "compute_capability": "Metal Performance Shaders",
        "device": "Apple Neural Engine" if hasattr(mx, 'metal') else "CPU",
    }


@app.post("/embedding")
async def embedding_endpoint(request: dict):
    """Generate embeddings for the given text using the loaded MLX model."""
    try:
        # Extract text from request - support various formats
        text = request.get("prompt") or request.get("input") or request.get("content")
        
        if not text:
            raise HTTPException(status_code=400, detail="Missing 'prompt', 'input', or 'content' field")
        
        if model_manager.model is None:
            raise HTTPException(status_code=400, detail="No model loaded. Call /load first.")
        
        # Handle both single string and list of strings
        if isinstance(text, list):
            embeddings = [model_manager.embed(t) for t in text]
        else:
            embeddings = [model_manager.embed(text)]
        
        return {
            "embeddings": embeddings,
            "model": model_manager.current_model_name,
        }
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Embedding error: {e}")
        raise HTTPException(status_code=500, detail=str(e))


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
