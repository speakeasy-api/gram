"""Presidio HTTP compatibility endpoint for Go's existing PII scanner client."""

from __future__ import annotations

import json
import math
from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager
from dataclasses import asdict

import click
import uvicorn
from starlette.applications import Starlette
from starlette.requests import Request
from starlette.responses import JSONResponse
from starlette.routing import Route

from pystreams.risk.clairic import ClairicResponseError, ClairicScanner
from pystreams.risk.scanner import ScanInputError


def build_app(scanner: ClairicScanner) -> Starlette:
    @asynccontextmanager
    async def lifespan(_app: Starlette) -> AsyncGenerator[None]:
        async with scanner:
            yield

    async def analyze(request: Request) -> JSONResponse:
        try:
            body = bytearray()
            async for chunk in request.stream():
                body.extend(chunk)
                if len(body) > 2 * 1024 * 1024:
                    return JSONResponse({"error": "request too large"}, status_code=413)
            payload = json.loads(body)
            if not isinstance(payload, dict):
                raise ValueError
            texts = payload.get("text")
            entities = payload.get("entities")
            threshold = payload.get("score_threshold", 0.5)
            if (
                payload.get("language", "en") != "en"
                or not isinstance(texts, list)
                or len(texts) > 32
                or any(not isinstance(text, str) for text in texts)
                or (
                    entities is not None
                    and (
                        not isinstance(entities, list)
                        or any(not isinstance(entity, str) for entity in entities)
                    )
                )
                or type(threshold) not in (int, float)
                or not math.isfinite(threshold)
                or not 0 <= threshold <= 1
            ):
                raise ValueError
            results = []
            for text in texts:
                results.append(
                    [
                        asdict(result)
                        for result in await scanner.recognize(text, entities, threshold)
                    ]
                )
            # Go applies the existing false-positive filters and byte conversion.
            return JSONResponse(results)
        except ValueError, ScanInputError:
            return JSONResponse({"error": "invalid scan input"}, status_code=400)
        except ClairicResponseError, TimeoutError:
            # Never expose upstream response bodies, credentials or scanned text.
            return JSONResponse({"error": "PII scan unavailable"}, status_code=502)

    async def health(_request: Request) -> JSONResponse:
        return JSONResponse({"status": "ok"})

    return Starlette(
        routes=[
            Route("/analyze", analyze, methods=["POST"]),
            Route("/healthz", health, methods=["GET"]),
        ],
        lifespan=lifespan,
    )


@click.command("clairic-server")
@click.option("--host", default="127.0.0.1", show_default=True)
@click.option("--port", default=8091, type=click.IntRange(1, 65535), show_default=True)
def cli(host: str, port: int) -> None:
    """Serve the internal PII adapter; keep it on a private network."""
    uvicorn.run(
        build_app(ClairicScanner.from_env()), host=host, port=port, access_log=False
    )
