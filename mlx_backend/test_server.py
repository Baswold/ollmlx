#!/usr/bin/env python3
"""
Test script for MLX Backend Server

This script tests the MLX backend service to ensure it:
1. Starts successfully
2. Accepts completion requests
3. Returns responses in Ollama-compatible format
4. Handles errors gracefully
"""

import asyncio
import json
import time
import requests
from pathlib import Path

# Note: This test assumes the server is running on localhost:8000
BASE_URL = "http://127.0.0.1:8000"
TIMEOUT = 30


def test_health_check():
    """Test /health endpoint"""
    print("Testing /health endpoint...")
    try:
        response = requests.get(f"{BASE_URL}/health", timeout=5)
        response.raise_for_status()
        data = response.json()
        print(f"✓ Health check passed: {data}")
        return True
    except Exception as e:
        print(f"✗ Health check failed: {e}")
        return False


def test_info():
    """Test /info endpoint"""
    print("\nTesting /info endpoint...")
    try:
        response = requests.get(f"{BASE_URL}/info", timeout=5)
        response.raise_for_status()
        data = response.json()
        print(f"✓ Info endpoint passed: {data}")
        return True
    except Exception as e:
        print(f"✗ Info endpoint failed: {e}")
        return False


def test_completion_streaming():
    """Test streaming completion endpoint"""
    print("\nTesting /completion endpoint with streaming...")

    request_data = {
        "prompt": "What is machine learning?",
        "options": {
            "temperature": 0.7,
            "top_k": 40,
            "top_p": 0.9,
            "num_predict": 20,
        }
    }

    try:
        response = requests.post(
            f"{BASE_URL}/completion",
            json=request_data,
            stream=True,
            timeout=TIMEOUT
        )
        response.raise_for_status()

        print("Receiving streaming response:")
        chunks = []
        for line in response.iter_lines():
            if line:
                data = json.loads(line)
                chunks.append(data)
                if data.get("done", False):
                    print(f"\n✓ Streaming completed: {len(chunks)} chunks received")
                    print(f"  Final response: {data}")
                    return True
                else:
                    # Print partial content
                    content = data.get("content", "")
                    print(content, end="", flush=True)

        print("\n✗ Streaming ended without 'done' flag")
        return False

    except requests.exceptions.Timeout:
        print(f"✗ Request timeout after {TIMEOUT} seconds")
        return False
    except Exception as e:
        print(f"✗ Completion test failed: {e}")
        return False


def test_response_format():
    """Verify response format matches Ollama's CompletionResponse"""
    print("\nTesting response format compatibility...")

    request_data = {
        "prompt": "Hello",
        "options": {
            "num_predict": 5,
        }
    }

    try:
        response = requests.post(
            f"{BASE_URL}/completion",
            json=request_data,
            stream=True,
            timeout=TIMEOUT
        )

        for line in response.iter_lines():
            if line:
                data = json.loads(line)
                # Verify required fields
                required_fields = [
                    "content",
                    "done",
                    "eval_duration",
                    "eval_count",
                ]
                for field in required_fields:
                    if field not in data:
                        print(f"✗ Missing required field: {field}")
                        return False

                if data.get("done", False):
                    print("✓ Response format is compatible")
                    return True

        return False
    except Exception as e:
        print(f"✗ Format test failed: {e}")
        return False


def main():
    """Run all tests"""
    print("=" * 60)
    print("MLX Backend Server Test Suite")
    print("=" * 60)

    tests = [
        test_health_check,
        test_info,
        test_response_format,
        test_completion_streaming,
    ]

    results = []
    for test in tests:
        try:
            results.append(test())
        except Exception as e:
            print(f"Unexpected error in test: {e}")
            results.append(False)

    print("\n" + "=" * 60)
    print(f"Results: {sum(results)}/{len(results)} tests passed")
    print("=" * 60)

    return all(results)


if __name__ == "__main__":
    import sys
    success = main()
    sys.exit(0 if success else 1)
