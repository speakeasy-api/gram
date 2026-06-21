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

import multiprocessing
import os
import time
from collections.abc import Sequence
from concurrent.futures import Future, ProcessPoolExecutor
from dataclasses import dataclass
from functools import partial
from typing import Protocol, Self, TypeVar

import anyio
from anyio import to_thread
from asyncer import asyncify
from presidio_analyzer import AnalyzerEngine
from presidio_analyzer.nlp_engine import NlpEngineProvider

from pystreams.risk import presidiofp

# The spaCy model bundled into the image (pinned in pystreams/pyproject.toml).
# Presidio's default AnalyzerEngine() would also load this model, but selecting
# it explicitly ties the scanner to the model we actually ship and stops a future
# Presidio default change from silently reaching for a model we don't package.
SPACY_MODEL = "en_core_web_lg"

_T = TypeVar("_T")


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
    process-pool one), and is also exposed as an async context manager so callers
    can ``async with`` the scanner instead of pairing it with a ``finally``.
    """

    async def scan(
        self, content: str, entities: list[str] | None
    ) -> list[Detection]: ...

    async def aclose(self) -> None: ...

    async def __aenter__(self) -> Self: ...

    async def __aexit__(self, *exc_info: object) -> None: ...


class _AsyncCloseable:
    """Mixin exposing a scanner's ``aclose`` as an async context manager.

    Both scanners release resources through ``aclose``; entering returns the
    scanner and leaving the block closes it, so a caller can ``async with`` the
    scanner rather than calling ``aclose`` from a ``finally``.
    """

    async def aclose(self) -> None: ...

    async def __aenter__(self) -> Self:
        return self

    async def __aexit__(self, *exc_info: object) -> None:
        await self.aclose()


class ThreadScanner(_AsyncCloseable):
    """Scan in-process on an anyio worker thread (the opt-out from the pool).

    Selected with ``--scan-workers 0``; the :class:`ProcessPoolScanner` is the
    default. Presidio's per-scan work is almost entirely GIL-bound, so extra scan
    threads don't add parallelism — they thrash the GIL and starve the event loop.
    A local burst sweep on a 10-core box found throughput/latency peak at 2
    concurrent scans and degrade monotonically with more (the default-40 thread
    pool managed only ~28 msg/s at p50 ~1.55s versus ~42 msg/s at p50 ~1.07s with
    2). The optimum tracks the GIL, not the core count, so 2 is a sane default
    everywhere; to scale past it, use a :class:`ProcessPoolScanner`.
    ``max_concurrency`` of None (or <=0) disables the cap and falls back to
    anyio's default thread-pool limiter (40).
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


class ProcessPoolScanner(_AsyncCloseable):
    """Scan in a pool of worker processes, each with its own GIL (the default).

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

    Two lifecycle knobs borrow from gunicorn's pre-fork model (the same shape the
    official Presidio image uses to scale):

    - ``max_tasks_per_child`` recycles a worker after it has run that many scans
      (gunicorn's ``--max-requests``), bounding spaCy/numpy memory drift over a
      long-lived worker. ``None``/<=0 disables recycling.
    - ``scan_timeout`` bounds how long a single scan may take before it is treated
      as a failure (gunicorn's ``--timeout``). The worker cannot be interrupted
      mid-scan, but the executor stops routing to it, ``max_tasks_per_child``
      recycles it once the doomed scan finishes, and meanwhile the other workers
      keep serving — a single pathological message removes one worker from
      rotation rather than stalling the consumer. ``None``/<=0 disables the bound.

    Everything that touches the executor is bridged through anyio's threading and
    cancellation primitives (``to_thread.run_sync`` + ``fail_after``) rather than
    the asyncio loop directly, so the scanner runs unchanged on either anyio
    backend (asyncio or trio). The bridge uses a dedicated thread limiter sized to
    the worker count, so blocking on pool results never draws down anyio's shared
    default thread pool (used by the publish hop and the in-process scanner) — only
    ``max_workers`` scans run at once, so that many result-waits is the ceiling.
    """

    def __init__(
        self,
        executor: ProcessPoolExecutor,
        *,
        scan_timeout: float | None = 30.0,
        wait_limiter: anyio.CapacityLimiter | None = None,
    ):
        self._executor = executor
        self._scan_timeout = scan_timeout if scan_timeout and scan_timeout > 0 else None
        # Dedicated limiter for the result-wait threads (see class docstring). None
        # falls back to anyio's default limiter — fine for the single-scan tests,
        # but ``create`` always supplies one for the real, concurrent path.
        self._wait_limiter = wait_limiter

    @classmethod
    async def create(
        cls,
        *,
        max_workers: int = 4,
        max_tasks_per_child: int | None = 1000,
        scan_timeout: float | None = 30.0,
    ) -> ProcessPoolScanner:
        """Build the pool and eagerly warm every worker's analyzer.

        ``forkserver`` is chosen explicitly so the start method does not depend on
        the platform default (and so ``max_tasks_per_child`` is usable — it is not
        supported with the ``fork`` start method). The warmup forces each worker to
        spawn and load its model up front, so the first real scans don't pay
        model-load latency.
        """
        executor = ProcessPoolExecutor(
            max_workers=max_workers,
            mp_context=multiprocessing.get_context("forkserver"),
            initializer=_worker_init,
            # <=0 (or None) means "live as long as the pool" — disable recycling.
            max_tasks_per_child=(
                max_tasks_per_child
                if max_tasks_per_child and max_tasks_per_child > 0
                else None
            ),
        )
        scanner = cls(
            executor,
            scan_timeout=scan_timeout,
            wait_limiter=anyio.CapacityLimiter(max_workers),
        )
        # One warmup task per worker, run concurrently; each does a real scan and
        # then briefly sleeps so the tasks can't all be served by one worker —
        # forcing the pool to spawn (and initialize) every worker before traffic.
        try:
            async with anyio.create_task_group() as tg:
                for _ in range(max_workers):
                    tg.start_soon(scanner._warm_one)
        except BaseException:
            # Warmup failed or was cancelled after spawning some workers; reap them
            # (aclose is bounded) so a failed create doesn't leak processes.
            await scanner.aclose()
            raise
        return scanner

    async def scan(self, content: str, entities: list[str] | None) -> list[Detection]:
        future = self._executor.submit(_worker_scan, content, entities)
        if self._scan_timeout is None:
            return await self._await_result(future)
        try:
            with anyio.fail_after(self._scan_timeout):
                return await self._await_result(future)
        except TimeoutError:
            # Pull it from the executor queue if it hasn't started; a no-op once a
            # worker has picked it up (the scan can't be interrupted, but the wait
            # is over and the worker will be recycled per max_tasks_per_child).
            future.cancel()
            raise

    async def _warm_one(self) -> None:
        await self._await_result(self._executor.submit(_worker_warm))

    async def _await_result(self, future: Future[_T]) -> _T:
        # Bridge a concurrent.futures future to anyio without binding to asyncio's
        # loop: a worker thread blocks on result() and the await is cancellable
        # (abandon_on_cancel) so fail_after can fire without waiting the scan out.
        # The dedicated limiter keeps these waits off anyio's shared thread pool.
        return await to_thread.run_sync(
            future.result, abandon_on_cancel=True, limiter=self._wait_limiter
        )

    async def aclose(self, *, grace_period: float = 10.0) -> None:
        """Shut the pool down, bounded so a stalled scan can't hang teardown.

        Cancels queued scans and waits up to ``grace_period`` seconds for in-flight
        ones (which can't be interrupted mid-scan) to finish; past the deadline the
        worker processes are killed so the surrounding teardown (e.g. the broker's
        publish flush) is never blocked indefinitely.
        """
        with anyio.move_on_after(grace_period) as scope:
            await to_thread.run_sync(
                partial(self._executor.shutdown, wait=True, cancel_futures=True),
                abandon_on_cancel=True,
            )
        if scope.cancelled_caught:
            for proc in list(getattr(self._executor, "_processes", {}).values()):
                proc.kill()


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
    results = analyzer.analyze(text=content, entities=entities, language="en")
    if not results:
        return []
    n = len(content)
    # Clamp each span to the content's bounds (guarding against an out-of-range
    # span) and drop catalog false positives (reserved/placeholder IPs and emails,
    # cloud/CDN ASN attribution) before they ever reach the handler. This pass
    # works in character offsets, so a discarded match never costs byte conversion.
    spans: list[tuple[Recognized, int, int, str]] = []
    for r in results:
        start = max(0, min(r.start, n))
        end = max(start, min(r.end, n))
        match = content[start:end]
        if presidiofp.reason(r.entity_type, match):
            continue
        spans.append((r, start, end, match))
    if not spans:
        return []
    # Presidio reports character offsets, but the Finding schema carries UTF-8 byte
    # positions. Resolve a byte offset only for the boundaries we actually emit —
    # at most two per surviving match — so memory stays O(matches) rather than the
    # O(length) a full per-character prefix table costs on every scan, which would
    # multiply across pool workers on large payloads.
    byte_at = _byte_offsets(content, spans)
    return [
        Detection(
            entity_type=r.entity_type,
            match=match,
            start_pos=byte_at[start],
            end_pos=byte_at[end],
            confidence=r.score,
        )
        for r, start, end, match in spans
    ]


def _byte_offsets(
    content: str, spans: list[tuple[Recognized, int, int, str]]
) -> dict[int, int]:
    """Map each span boundary's character offset to its UTF-8 byte offset.

    ASCII text encodes one byte per character, so the offsets coincide and no
    conversion is needed. Otherwise the string is walked once in offset order,
    accumulating the byte position only at the boundaries we need: O(length) time
    but O(matches) memory, versus the O(length) memory a full prefix table costs.
    """
    needed: set[int] = set()
    for _, start, end, _ in spans:
        needed.add(start)
        needed.add(end)
    if content.isascii():
        return {pos: pos for pos in needed}
    byte_at: dict[int, int] = {}
    byte_pos = 0
    char_pos = 0
    for target in sorted(needed):
        byte_pos += len(content[char_pos:target].encode("utf-8"))
        char_pos = target
        byte_at[target] = byte_pos
    return byte_at


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
