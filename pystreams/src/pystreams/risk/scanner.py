"""Presidio PII scanning machinery, encapsulated behind a tiny async API.

This module owns everything to do with *running* Presidio: loading the spaCy
model, building the ``AnalyzerEngine``, running the scan off the event loop
(either on a worker thread or across a pool of worker processes), dropping
catalog false positives, and converting match spans to byte offsets. It exposes
that as a single :class:`Scanner` protocol — ``await scanner.scan(content,
entities)`` returns the real :class:`Detection` matches, and ``await
scanner.aclose()`` releases any resources.

Everything *downstream* of a detection — turning it into a ``Finding``, logging,
and publishing to Pub/Sub — lives in ``handler.py``. The split keeps the
GIL-bound, model-heavy scan concern isolated from the I/O and domain-mapping
concern, and lets each be tested on its own (the handler against a fake scanner,
the scanner against a fake analyzer).

Two scanners are provided:

- :class:`ThreadScanner` runs the scan in-process on an anyio worker thread. The
  scan is almost entirely GIL-bound, so a single process tops out around
  ~50 msg/s no matter how many scan threads are allowed; the thread count is
  capped low for that reason.
- :class:`ProcessPoolScanner` runs the scan across worker processes, each with
  its own GIL and its own model, breaking that single-process ceiling inside one
  pod.
"""

from __future__ import annotations

import asyncio
import multiprocessing
import os
import time
from collections.abc import Sequence
from concurrent.futures import ProcessPoolExecutor
from dataclasses import dataclass
from typing import Protocol

import anyio
from asyncer import asyncify
from presidio_analyzer import AnalyzerEngine
from presidio_analyzer.nlp_engine import NlpEngineProvider

from pystreams.risk import presidiofp

# The spaCy model bundled into the image (pinned in pystreams/pyproject.toml).
# Presidio's default AnalyzerEngine() would also load this model, but selecting
# it explicitly ties the scanner to the model we actually ship and stops a future
# Presidio default change from silently reaching for a model we don't package.
SPACY_MODEL = "en_core_web_lg"


@dataclass(frozen=True)
class Detection:
    """A real (non-false-positive) PII match found in scanned content.

    The scanner's output unit and the whole of its public data contract: byte
    offsets (the schema the ``Finding`` carries) and the matched substring. The
    handler maps this onto a ``Finding`` — deriving the rule id, stamping context,
    etc. — so the rule-naming and finding-building stay out of the scanner.

    Picklable (a frozen dataclass of primitives) so it can travel back from a
    :class:`ProcessPoolScanner` worker process.
    """

    entity_type: str
    match: str
    start_pos: int  # UTF-8 byte offset.
    end_pos: int  # UTF-8 byte offset.
    confidence: float  # Detection confidence, 0.0-1.0.


class Scanner(Protocol):
    """Turns scanned content into the real PII detections, off the event loop.

    Implementations run the analyzer on a worker thread or in a separate process
    and return the post-false-positive-filter matches. ``aclose`` releases any
    resources (a no-op for the in-process scanner; drains the pool for the
    process-pool one).
    """

    async def scan(
        self, content: str, entities: list[str] | None
    ) -> list[Detection]: ...

    async def aclose(self) -> None: ...


class ThreadScanner:
    """Scan in-process on an anyio worker thread (the default).

    Presidio's per-scan work is almost entirely GIL-bound, so extra scan threads
    don't add parallelism — they thrash the GIL and starve the event loop. A local
    burst sweep on a 10-core box found throughput/latency peak at 2 concurrent
    scans and degrade monotonically with more (the default-40 thread pool managed
    only ~28 msg/s at p50 ~1.55s versus ~42 msg/s at p50 ~1.07s with 2). The
    optimum tracks the GIL, not the core count, so 2 is a sane default everywhere;
    to scale past it, use a :class:`ProcessPoolScanner`. ``max_concurrency`` of
    None (or <=0) disables the cap and falls back to anyio's default thread-pool
    limiter (40).
    """

    def __init__(self, analyzer: Analyzer, *, max_concurrency: int | None = 2):
        self._analyzer = analyzer
        self._max_concurrency = max_concurrency
        self._limiter: anyio.CapacityLimiter | None = None

    def _get_limiter(self) -> anyio.CapacityLimiter | None:
        """Lazily build the shared scan limiter (needs a running event loop)."""
        if self._max_concurrency is None or self._max_concurrency <= 0:
            return None
        if self._limiter is None:
            self._limiter = anyio.CapacityLimiter(self._max_concurrency)
        return self._limiter

    async def scan(self, content: str, entities: list[str] | None) -> list[Detection]:
        return await asyncify(_scan_to_detections, limiter=self._get_limiter())(
            self._analyzer, content, entities
        )

    async def aclose(self) -> None:
        return None


class ProcessPoolScanner:
    """Scan in a pool of worker processes, each with its own GIL.

    The single-process throughput ceiling is the GIL: the spaCy NER pass and
    Presidio's regex recognizers hold it for almost the whole scan, so no number
    of in-process threads gets past ~50 msg/s. Running the scan in a
    ``ProcessPoolExecutor`` gives each worker its own interpreter and GIL, so N
    workers scan genuinely in parallel and the event loop never blocks on scan
    work — breaking the ceiling inside a single pod, without adding replicas.

    Each worker loads its own ``AnalyzerEngine`` once, in the pool initializer, and
    reuses it for every scan it handles. The model is loaded per worker rather than
    shared copy-on-write: Python 3.14 defaults the start method to ``forkserver``
    (and forking a process that already runs the asyncio loop + gRPC client threads
    is unsafe anyway), so a clean per-worker load is the robust choice. The cost is
    ~one model resident set per worker; keep the worker count small (2-4).

    Workers return only the final :class:`Detection` list (already
    false-positive-filtered, byte offsets resolved), which is small and picklable;
    the heavy text and the analyzer never cross the process boundary per message.
    """

    def __init__(self, executor: ProcessPoolExecutor):
        self._executor = executor

    @classmethod
    async def create(cls, *, max_workers: int = 4) -> ProcessPoolScanner:
        """Build the pool and eagerly warm every worker's analyzer.

        ``forkserver`` is chosen explicitly so the start method does not depend on
        the platform default. The warmup forces each worker to spawn and load its
        model up front, so the first real scans don't pay model-load latency.
        """
        executor = ProcessPoolExecutor(
            max_workers=max_workers,
            mp_context=multiprocessing.get_context("forkserver"),
            initializer=_worker_init,
        )
        loop = asyncio.get_running_loop()
        # One warmup task per worker, run concurrently; each does a real scan and
        # then briefly sleeps so the tasks can't all be served by one worker —
        # forcing the pool to spawn (and initialize) every worker before traffic.
        await asyncio.gather(
            *(loop.run_in_executor(executor, _worker_warm) for _ in range(max_workers))
        )
        return cls(executor)

    async def scan(self, content: str, entities: list[str] | None) -> list[Detection]:
        loop = asyncio.get_running_loop()
        return await loop.run_in_executor(
            self._executor, _worker_scan, content, entities
        )

    async def aclose(self) -> None:
        # Wait for in-flight scans so a shutdown doesn't drop a message mid-scan.
        await asyncify(self._executor.shutdown)(wait=True)


# --- Worker-process state and entry points -------------------------------------
#
# These run in the ProcessPoolScanner's worker processes. Each worker builds its
# own analyzer once (in the initializer) and stores it in this module-level global,
# then reuses it for every scan. The functions are module-level (not closures or
# methods) so ``forkserver``/``spawn`` can import them by qualified name.

_WORKER_ANALYZER: Analyzer | None = None


def _worker_init() -> None:
    """Pool initializer: build this worker's analyzer once, up front."""
    global _WORKER_ANALYZER
    _WORKER_ANALYZER = _build_analyzer()


def _worker_scan(content: str, entities: list[str] | None) -> list[Detection]:
    """Scan in a worker process using its cached analyzer."""
    global _WORKER_ANALYZER
    analyzer = _WORKER_ANALYZER
    if analyzer is None:
        # Defensive: the initializer always runs first, but rebuild rather than
        # crash the worker if somehow it didn't.
        analyzer = _WORKER_ANALYZER = _build_analyzer()
    return _scan_to_detections(analyzer, content, entities)


def _worker_warm() -> int:
    """Exercise the worker's analyzer (loaded by the initializer) and yield.

    The short sleep lets concurrently-submitted warmups land on distinct workers,
    so ``create`` reliably spins up every worker before serving real traffic.
    """
    _worker_scan("warm up: a@b.com", None)
    time.sleep(0.25)
    return os.getpid()


def _scan_to_detections(
    analyzer: Analyzer, content: str, entities: list[str] | None
) -> list[Detection]:
    """Analyze the content and return the real (non-false-positive) matches.

    Pure and side-effect free so it can run either on an anyio worker thread (the
    in-process scanner) or inside a pool worker process (the process-pool scanner):
    the analyzer call, the false-positive classification (which may consult the
    embedded ASN database), and the byte-offset conversion all stay off the event
    loop wherever it runs.
    """
    detections: list[Detection] = []
    for r in analyzer.analyze(text=content, entities=entities, language="en"):
        start_byte, end_byte, match = _byte_span(content, r.start, r.end)
        # Drop catalog false positives (reserved/placeholder IPs and emails,
        # cloud/CDN ASN attribution) before they ever reach the handler.
        if presidiofp.reason(r.entity_type, match):
            continue
        detections.append(
            Detection(
                entity_type=r.entity_type,
                match=match,
                start_pos=start_byte,
                end_pos=end_byte,
                confidence=r.score,
            )
        )
    return detections


def _build_analyzer() -> AnalyzerEngine:
    """Construct an AnalyzerEngine backed by the explicitly selected spaCy model.

    Synchronous: callable directly inside a pool worker's initializer and wrapped
    in ``asyncify`` by :func:`build_default_analyzer` for the async caller.
    """
    provider = NlpEngineProvider(
        nlp_configuration={
            "nlp_engine_name": "spacy",
            "models": [{"lang_code": "en", "model_name": SPACY_MODEL}],
        }
    )
    return AnalyzerEngine(nlp_engine=provider.create_engine())


async def build_default_analyzer() -> AnalyzerEngine:
    """Construct an AnalyzerEngine off the event loop (model load is blocking)."""
    return await asyncify(_build_analyzer)()


def _byte_span(content: str, start: int, end: int) -> tuple[int, int, str]:
    """Clamp a Presidio character span and convert it to UTF-8 byte offsets.

    Presidio reports character (code point) offsets, but the Finding schema
    carries byte positions. Offsets are clamped to the content's bounds first to
    guard against an out-of-range span. Returns ``(start_byte, end_byte, match)``.
    """
    n = len(content)
    start = max(0, min(start, n))
    end = max(start, min(end, n))
    start_byte = len(content[:start].encode("utf-8"))
    end_byte = len(content[:end].encode("utf-8"))
    return start_byte, end_byte, content[start:end]


class Recognized(Protocol):
    """The slice of Presidio's ``RecognizerResult`` the scanner consumes."""

    entity_type: str
    start: int  # Character offset (inclusive) of the match in the scanned text.
    end: int  # Character offset (exclusive) of the match in the scanned text.
    score: float  # Detection confidence, 0.0-1.0.


class Analyzer(Protocol):
    """The slice of ``AnalyzerEngine`` the scanner depends on.

    Narrowing to a protocol keeps the engine injectable — tests can supply a
    lightweight fake instead of loading Presidio's NLP model.
    """

    def analyze(
        self, *, text: str, entities: list[str] | None, language: str
    ) -> Sequence[Recognized]: ...
