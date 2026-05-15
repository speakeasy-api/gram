# syntax=docker/dockerfile:1.7
FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim@sha256:e5b65587bce7de595f299855d7385fe7fca39b8a74baa261ba1b7147afa78e58 AS builder

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    HF_HOME=/opt/hf-cache \
    HF_HUB_DISABLE_TELEMETRY=1 \
    TOKENIZERS_PARALLELISM=false \
    OMP_NUM_THREADS=2 \
    MODEL_REPO=protectai/deberta-v3-base-prompt-injection-v2 \
    MODEL_REVISION=e6535ca4ce3ba852083e75ec585d7c8aeb4be4c5 \
    PATH="/app/.venv/bin:$PATH"

WORKDIR /app

COPY pyproject.toml uv.lock ./
RUN uv sync --locked --no-dev --no-install-project

# Bake the model into the image so the runtime container is offline deterministic.
# Only ONNX weights + tokenizer artifacts are pulled; skip the safetensors copy.
RUN huggingface-cli download "${MODEL_REPO}" \
      --revision "${MODEL_REVISION}" \
      --include "onnx/model.onnx" "tokenizer*.json" "spm.model" "config.json" "special_tokens_map.json" \
 && python -c "import os; from optimum.onnxruntime import ORTModelForSequenceClassification; from transformers import AutoTokenizer; \
repo=os.environ['MODEL_REPO']; rev=os.environ['MODEL_REVISION']; \
ORTModelForSequenceClassification.from_pretrained(repo, subfolder='onnx', revision=rev); \
AutoTokenizer.from_pretrained(repo, revision=rev)"

FROM python:3.12-slim@sha256:ec948fa5f90f4f8907e89f4800cfd2d2e91e391a4bce4a6afa77ba265bc3a2fe AS runtime

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    HF_HOME=/opt/hf-cache \
    HF_HUB_DISABLE_TELEMETRY=1 \
    HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1 \
    HF_LOCAL_FILES_ONLY=1 \
    TOKENIZERS_PARALLELISM=false \
    OMP_NUM_THREADS=2 \
    MODEL_REPO=protectai/deberta-v3-base-prompt-injection-v2 \
    MODEL_REVISION=e6535ca4ce3ba852083e75ec585d7c8aeb4be4c5 \
    PATH="/app/.venv/bin:$PATH"

WORKDIR /app

RUN useradd --system --no-create-home --uid 10001 piclassifier

COPY --from=builder --chown=10001:10001 /app/.venv /app/.venv
COPY --from=builder --chown=10001:10001 /opt/hf-cache /opt/hf-cache
COPY --chown=10001:10001 app.py ./

USER piclassifier
EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8000/health', timeout=3).read()"

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000", "--workers", "1"]
