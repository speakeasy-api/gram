# syntax=docker/dockerfile:1.7
FROM python:3.12-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    HF_HOME=/opt/hf-cache \
    HF_HUB_DISABLE_TELEMETRY=1 \
    TOKENIZERS_PARALLELISM=false \
    OMP_NUM_THREADS=2 \
    MODEL_REPO=protectai/deberta-v3-base-prompt-injection-v2

WORKDIR /app

RUN apt-get update \
 && apt-get install -y --no-install-recommends curl \
 && rm -rf /var/lib/apt/lists/*

COPY requirements.txt ./
RUN pip install -r requirements.txt

# Bake the model into the image so the runtime container is offline-deterministic.
# Only ONNX weights + tokenizer artifacts are pulled — skip the safetensors copy.
RUN huggingface-cli download "${MODEL_REPO}" \
      --include "onnx/model.onnx" "tokenizer*.json" "spm.model" "config.json" "special_tokens_map.json" \
 && python -c "from optimum.onnxruntime import ORTModelForSequenceClassification; from transformers import AutoTokenizer; \
ORTModelForSequenceClassification.from_pretrained('protectai/deberta-v3-base-prompt-injection-v2', subfolder='onnx'); \
AutoTokenizer.from_pretrained('protectai/deberta-v3-base-prompt-injection-v2')"

COPY app.py ./

RUN useradd --system --no-create-home --uid 10001 piclassifier \
 && chown -R piclassifier:piclassifier /opt/hf-cache /app

USER piclassifier
EXPOSE 8000

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000", "--workers", "1"]
