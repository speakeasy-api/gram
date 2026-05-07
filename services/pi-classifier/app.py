"""Prompt-injection ML classifier HTTP service.

Serves protectai/deberta-v3-base-prompt-injection-v2 over a single batched
/detect endpoint. The Go server in `gram` calls this when a risk policy has
the `deberta-v3-classifier` rule selected under its `prompt_injection` source.
"""

from __future__ import annotations

import logging
import os
import sys
import time
from contextlib import asynccontextmanager
from typing import Literal

import numpy as np
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse
from optimum.onnxruntime import ORTModelForSequenceClassification
from pydantic import BaseModel, Field, field_validator
from transformers import AutoTokenizer

MODEL_REPO = os.environ.get("MODEL_REPO", "protectai/deberta-v3-base-prompt-injection-v2")
MAX_LENGTH = int(os.environ.get("MAX_LENGTH", "512"))
MAX_BATCH = int(os.environ.get("MAX_BATCH", "64"))
MAX_BODY_BYTES = int(os.environ.get("MAX_BODY_BYTES", str(2 * 1024 * 1024)))

logging.basicConfig(
    level=logging.INFO,
    format='{"ts":"%(asctime)s","level":"%(levelname)s","logger":"%(name)s","msg":%(message)r}',
    stream=sys.stdout,
)
log = logging.getLogger("pi-classifier")


class _State:
    model: ORTModelForSequenceClassification | None = None
    tokenizer: AutoTokenizer | None = None
    inj_idx: int = 1


@asynccontextmanager
async def lifespan(_app: FastAPI):
    t0 = time.perf_counter()
    log.info(f"loading model repo={MODEL_REPO}")
    _State.tokenizer = AutoTokenizer.from_pretrained(MODEL_REPO)
    _State.model = ORTModelForSequenceClassification.from_pretrained(MODEL_REPO, subfolder="onnx")
    # Resolve the index of the INJECTION class once. id2label is on the config; default
    # for this model is {0: "SAFE", 1: "INJECTION"} but we don't want to hardcode it.
    id2label = {int(k): v for k, v in _State.model.config.id2label.items()}
    inj = next((i for i, lbl in id2label.items() if lbl.upper() == "INJECTION"), 1)
    _State.inj_idx = inj
    log.info(f"model loaded in {time.perf_counter() - t0:.2f}s id2label={id2label} inj_idx={inj}")
    yield


app = FastAPI(title="gram-pi-classifier", version="1", lifespan=lifespan)


@app.middleware("http")
async def _enforce_body_limit(request: Request, call_next):
    cl = request.headers.get("content-length")
    if cl is not None and cl.isdigit() and int(cl) > MAX_BODY_BYTES:
        return JSONResponse({"detail": f"body too large (limit {MAX_BODY_BYTES} bytes)"}, status_code=413)
    return await call_next(request)


class DetectRequest(BaseModel):
    texts: list[str] = Field(..., description="Inputs to classify; runs as a single batched forward pass.")

    @field_validator("texts")
    @classmethod
    def _bounded(cls, v: list[str]) -> list[str]:
        if not v:
            raise ValueError("texts must contain at least one item")
        if len(v) > MAX_BATCH:
            raise ValueError(f"texts length {len(v)} exceeds MAX_BATCH={MAX_BATCH}")
        return v


class DetectResult(BaseModel):
    label: Literal["INJECTION", "SAFE"]
    score: float


class DetectResponse(BaseModel):
    results: list[DetectResult]


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/detect", response_model=DetectResponse)
def detect(req: DetectRequest) -> DetectResponse:
    if _State.model is None or _State.tokenizer is None:
        raise HTTPException(status_code=503, detail="model not loaded")

    enc = _State.tokenizer(
        req.texts,
        padding=True,
        truncation=True,
        max_length=MAX_LENGTH,
        return_tensors="np",
    )

    t0 = time.perf_counter()
    outputs = _State.model(**enc)
    dt_ms = (time.perf_counter() - t0) * 1000.0

    logits = outputs.logits  # shape (N, 2)
    probs = _softmax(logits)
    inj = int(_State.inj_idx)

    results: list[DetectResult] = []
    for row in probs:
        inj_score = float(row[inj])
        results.append(
            DetectResult(
                label="INJECTION" if inj_score >= 0.5 else "SAFE",
                score=inj_score,
            )
        )

    log.info(f"detect batch={len(req.texts)} inference_ms={dt_ms:.1f}")
    return DetectResponse(results=results)


def _softmax(logits: np.ndarray) -> np.ndarray:
    shifted = logits - logits.max(axis=-1, keepdims=True)
    exp = np.exp(shifted)
    return exp / exp.sum(axis=-1, keepdims=True)
