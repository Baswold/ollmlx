#!/usr/bin/env python3
"""Unit tests for MLXModelManager."""

import importlib
import os
import sys
from pathlib import Path
from types import ModuleType
from unittest import TestCase, mock


class TestMLXModelManager(TestCase):
    def test_model_path_respects_environment_variable(self):
        """Ensure OLLAMA_MODELS overrides the default model path."""

        mock_mx = ModuleType("mlx")
        mock_mx_core = ModuleType("mlx.core")
        mock_mx_core.gpu = object()
        mock_mx_core.set_default_device = mock.Mock()
        mock_mx.core = mock_mx_core
        mock_mx.__path__ = []

        mock_mlx_lm = ModuleType("mlx_lm")
        mock_mlx_lm.load = mock.Mock(return_value=(None, None))
        mock_tokenizer_utils = ModuleType("mlx_lm.tokenizer_utils")
        mock_tokenizer_utils.TokenizerWrapper = mock.Mock()
        mock_sample_utils = ModuleType("mlx_lm.sample_utils")
        mock_sample_utils.make_sampler = mock.Mock()
        mock_mlx_lm.__path__ = []

        module_overrides = {
            "mlx": mock_mx,
            "mlx.core": mock_mx_core,
            "mlx_lm": mock_mlx_lm,
            "mlx_lm.tokenizer_utils": mock_tokenizer_utils,
            "mlx_lm.sample_utils": mock_sample_utils,
        }

        custom_path = Path("/tmp/custom_ollama_models")

        env_overrides = {
            "OLLAMA_MODELS": str(custom_path),
            "HUGGINGFACE_HUB_CACHE": "",
            "HF_HOME": "",
        }

        with mock.patch.dict(sys.modules, module_overrides), mock.patch.dict(
            os.environ, env_overrides, clear=False
        ):
            sys.modules.pop("mlx_backend.server", None)
            server = importlib.import_module("mlx_backend.server")
            manager = server.MLXModelManager()

            self.assertEqual(manager.model_path, custom_path)
            self.assertEqual(os.environ["HUGGINGFACE_HUB_CACHE"], str(custom_path))
            self.assertEqual(os.environ["HF_HOME"], str(custom_path))

