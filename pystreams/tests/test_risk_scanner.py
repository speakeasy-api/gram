import threading
import time
from concurrent.futures import Future, ProcessPoolExecutor
from typing import cast

import anyio
import pytest

from pystreams.risk.scanner import (
    Detection,
    ProcessPoolScanner,
    Recognized,
    ThreadScanner,
    _AsyncCloseable,
)


class _Result:
    """Minimal stand-in for a Presidio ``RecognizerResult``."""

    def __init__(
        self, entity_type: str, start: int = 0, end: int = 0, score: float = 0.5
    ):
        self.entity_type = entity_type
        self.start = start
        self.end = end
        self.score = score


class FakeAnalyzer:
    """Records calls and returns canned detections keyed by input text.

    Lets the scanner tests run without loading Presidio's NLP model and keeps
    detection results deterministic.
    """

    def __init__(self, detections: dict[str, list[Recognized]] | None = None):
        self.detections = detections or {}
        self.calls: list[tuple[str, list[str] | None]] = []

    def analyze(
        self,
        *,
        text: str,
        entities: list[str] | None,
        language: str,
        score_threshold: float,
    ) -> list[Recognized]:
        self.calls.append((text, entities))
        assert language == "en"
        assert 0.0 <= score_threshold <= 1.0
        return self.detections.get(text, [])


def _scanner(analyzer: FakeAnalyzer) -> ThreadScanner:
    return ThreadScanner(analyzer)


async def test_maps_recognizer_results_to_detections():
    content = "email me at a@b.com"
    analyzer = FakeAnalyzer(
        {content: [_Result("EMAIL_ADDRESS", start=12, end=19, score=0.85)]}
    )

    (detection,) = await _scanner(analyzer).scan(content, None, 0.75)

    assert detection.entity_type == "EMAIL_ADDRESS"
    assert detection.match == "a@b.com"
    assert detection.start_pos == 12
    assert detection.end_pos == 19
    assert detection.confidence == 0.85


async def test_returns_one_detection_per_recognized_span():
    content = "call 555-0100 or 555-0199"
    analyzer = FakeAnalyzer(
        {
            content: [
                _Result("PHONE_NUMBER", start=5, end=13),
                _Result("PHONE_NUMBER", start=17, end=25),
            ]
        }
    )

    detections = await _scanner(analyzer).scan(content, None, 0.75)

    assert [d.match for d in detections] == ["555-0100", "555-0199"]


async def test_byte_offsets_for_multibyte_content():
    # "café " is 5 characters but 6 UTF-8 bytes; a match after it must be
    # reported in byte positions, not character positions.
    content = "café a@b.com"
    analyzer = FakeAnalyzer(
        {content: [_Result("EMAIL_ADDRESS", start=5, end=12, score=0.9)]}
    )

    (detection,) = await _scanner(analyzer).scan(content, None, 0.75)

    assert detection.match == "a@b.com"
    assert detection.start_pos == 6  # one extra byte from the 'é'
    assert detection.end_pos == 13


async def test_byte_offsets_for_multiple_multibyte_matches():
    # Two matches straddling separate multibyte runs exercise the single-pass
    # offset walk: each '€' is 3 UTF-8 bytes, so byte offsets diverge from char
    # offsets cumulatively and out of insertion order.
    content = "€ a@b.com € c@d.com"
    analyzer = FakeAnalyzer(
        {
            content: [
                # Reversed vs. text order to confirm the walk doesn't assume sorted
                # recognizer results.
                _Result("EMAIL_ADDRESS", start=12, end=19, score=0.9),
                _Result("EMAIL_ADDRESS", start=2, end=9, score=0.9),
            ]
        }
    )

    detections = await _scanner(analyzer).scan(content, None, 0.75)

    by_match = {d.match: d for d in detections}
    assert by_match["a@b.com"].start_pos == 4  # 3-byte '€' + space
    assert by_match["a@b.com"].end_pos == 11
    assert by_match["c@d.com"].start_pos == 16  # second '€' adds two more bytes
    assert by_match["c@d.com"].end_pos == 23


async def test_false_positives_are_filtered():
    # "10.0.0.1" is RFC1918 space and is dropped; "a@b.com" is a real match.
    content = "10.0.0.1 and a@b.com"
    analyzer = FakeAnalyzer(
        {
            content: [
                _Result("IP_ADDRESS", start=0, end=8),
                _Result("EMAIL_ADDRESS", start=13, end=20, score=0.7),
            ]
        }
    )

    detections = await _scanner(analyzer).scan(content, None, 0.75)

    # Only the real match survives; the reserved IP is dropped at the scanner.
    (detection,) = detections
    assert detection.entity_type == "EMAIL_ADDRESS"
    assert detection.match == "a@b.com"


async def test_all_false_positives_yields_no_detections():
    content = "reach me at user@example.com"
    analyzer = FakeAnalyzer({content: [_Result("EMAIL_ADDRESS", start=12, end=28)]})

    # example.com is a placeholder domain: filtered out entirely.
    assert await _scanner(analyzer).scan(content, None, 0.75) == []


async def test_nothing_recognized_yields_no_detections():
    analyzer = FakeAnalyzer()  # recognizes nothing

    assert await _scanner(analyzer).scan("nothing sensitive here", None, 0.75) == []


async def test_requested_entities_forwarded_to_analyzer():
    analyzer = FakeAnalyzer({"a@b.com": [_Result("EMAIL_ADDRESS", start=0, end=7)]})

    await _scanner(analyzer).scan("a@b.com", ["EMAIL_ADDRESS", "PHONE_NUMBER"], 0.75)

    # The explicit request set is passed through to the analyzer verbatim.
    assert analyzer.calls == [("a@b.com", ["EMAIL_ADDRESS", "PHONE_NUMBER"])]


async def test_none_entities_forwarded_to_analyzer():
    analyzer = FakeAnalyzer({"a@b.com": [_Result("EMAIL_ADDRESS", start=0, end=7)]})

    await _scanner(analyzer).scan("a@b.com", None, 0.75)

    # None tells Presidio to scan every type; it is forwarded unchanged.
    assert analyzer.calls == [("a@b.com", None)]


async def test_thread_scanner_is_async_context_manager():
    analyzer = FakeAnalyzer({"a@b.com": [_Result("EMAIL_ADDRESS", start=0, end=7)]})

    # Entering yields the scanner; the block can scan and leaving closes it.
    async with ThreadScanner(analyzer) as scanner:
        (detection,) = await scanner.scan("a@b.com", None, 0.75)
        assert detection.match == "a@b.com"


async def test_context_manager_exit_closes_scanner():
    closed = False

    class _RecordingScanner(_AsyncCloseable):
        async def aclose(self) -> None:
            nonlocal closed
            closed = True

    async with _RecordingScanner() as scanner:
        assert isinstance(scanner, _RecordingScanner)
        assert not closed
    # __aexit__ awaits the subclass's aclose, even on a normal exit.
    assert closed


class _StuckExecutor:
    """Executor stand-in whose submitted futures never complete.

    Lets the ProcessPoolScanner timeout path be tested without spawning real
    worker processes or loading the spaCy model: ``scan`` submits, then waits on a
    future that is never resolved, so the anyio ``fail_after`` deadline must fire.
    """

    def __init__(self):
        self.submitted: list[Future] = []

    def submit(self, fn, *args):
        future: Future = Future()
        self.submitted.append(future)
        return future


class _ImmediateExecutor:
    """Executor stand-in whose futures are already resolved to a canned value."""

    def __init__(self, result: list[Detection]):
        self._result = result

    def submit(self, fn, *args):
        future: Future = Future()
        future.set_result(self._result)
        return future


async def test_pool_scan_times_out_via_anyio_deadline():
    executor = _StuckExecutor()
    scanner = ProcessPoolScanner(cast(ProcessPoolExecutor, executor), scan_timeout=0.05)

    # The scan never completes, so the anyio fail_after deadline must raise.
    with pytest.raises(TimeoutError):
        await scanner.scan("anything", None, 0.75)

    # On timeout the future is cancelled, so a still-queued scan is pulled rather
    # than left to run with nobody awaiting it (and the abandoned wait unblocks).
    (future,) = executor.submitted
    assert future.cancelled()


async def test_pool_scan_without_timeout_returns_result():
    detection = Detection(
        entity_type="EMAIL_ADDRESS",
        match="a@b.com",
        start_pos=0,
        end_pos=7,
        confidence=0.5,
    )
    scanner = ProcessPoolScanner(
        cast(ProcessPoolExecutor, _ImmediateExecutor([detection])), scan_timeout=None
    )

    # scan_timeout=None disables the deadline; the result passes straight through.
    assert await scanner.scan("a@b.com", None, 0.75) == [detection]


class _WarmupFailExecutor:
    """Executor stand-in whose every task fails, to drive the warmup-error path.

    Records shutdown so the test can assert the pool is reaped when warmup raises.
    """

    def __init__(self, **kwargs):
        self.shutdowns: list[tuple[bool, bool]] = []

    def submit(self, fn, *args):
        future: Future = Future()
        future.set_exception(RuntimeError("warmup boom"))
        return future

    def shutdown(self, wait=True, cancel_futures=False):
        self.shutdowns.append((wait, cancel_futures))


async def test_pool_create_reaps_workers_when_warmup_fails(monkeypatch):
    executor = _WarmupFailExecutor()
    monkeypatch.setattr(
        "pystreams.risk.scanner.ProcessPoolExecutor", lambda **kw: executor
    )

    # Warmup raises -> create must shut the executor down before propagating, so a
    # failed create can't leak the workers it already spawned.
    with pytest.raises(BaseExceptionGroup, match="unhandled errors"):
        await ProcessPoolScanner.create(max_workers=2)

    assert executor.shutdowns, "executor was not shut down on warmup failure"


class _SlowShutdownExecutor:
    """Executor stand-in whose ``shutdown`` overruns the grace period.

    Has no ``_processes`` attribute, so it stands in for a CPython release that
    renamed the private internal ``aclose`` reaches for: the hard-kill fallback
    must degrade to a no-op rather than raising ``AttributeError`` and masking the
    teardown stall it was trying to recover from.
    """

    def shutdown(self, wait=True, cancel_futures=False):
        # Block past the tiny grace_period the test passes so move_on_after fires.
        time.sleep(5)


async def test_aclose_kill_path_degrades_when_processes_attr_missing():
    scanner = ProcessPoolScanner(
        cast(ProcessPoolExecutor, _SlowShutdownExecutor()), scan_timeout=None
    )

    # The graceful shutdown overruns, so aclose falls through to the hard-kill
    # path; with no ``_processes`` to read it must return cleanly, not raise.
    with anyio.fail_after(2):
        await scanner.aclose(grace_period=0.05)


class _DeferredExecutor:
    """Executor stand-in that withholds results until every scan is in flight.

    Each ``submit`` (driven from a worker thread) captures the scanned content
    beside the ``Future`` it returns and leaves it unresolved. Only once all
    ``expected`` scans have submitted does the final one complete them all — in
    reverse submission order, so completion order deliberately differs from caller
    order. This proves each scan receives only its own result, without spawning
    real workers or loading the spaCy model, and self-completes so the test needs
    no event-loop polling.
    """

    def __init__(self, expected: int):
        self._expected = expected
        self._lock = threading.Lock()
        self.pending: list[tuple[str, Future]] = []

    def submit(self, fn, *args):
        content = cast(str, args[0])
        future: Future = Future()
        with self._lock:
            self.pending.append((content, future))
            # The submit that completes the set resolves every future; the others
            # have all appended by now, so iterating outside the lock is safe.
            ready = list(self.pending) if len(self.pending) == self._expected else None
        if ready is not None:
            for queued_content, queued_future in reversed(ready):
                queued_future.set_result([_email_detection(queued_content)])
        return future


def _email_detection(content: str) -> Detection:
    """A detection whose ``match`` is the content, so a crossed result is visible."""
    return Detection(
        entity_type="EMAIL_ADDRESS",
        match=content,
        start_pos=0,
        end_pos=len(content),
        confidence=0.5,
    )


async def test_concurrent_scans_only_receive_their_own_results():
    # Routing is by Future identity, not shared state: each scan awaits exactly the
    # future its own submit returned. Drive many scans concurrently; the executor
    # holds every result until all are in flight, then completes them in reverse
    # order (so completion order differs from caller order) and we assert no caller
    # ever sees another scan's result. A refactor that introduced a shared "last
    # result" slot would cross wires here.
    contents = [f"user{i}@host{i}.test" for i in range(8)]
    executor = _DeferredExecutor(expected=len(contents))
    scanner = ProcessPoolScanner(cast(ProcessPoolExecutor, executor), scan_timeout=None)

    results: dict[str, list[Detection]] = {}

    async def run(content: str) -> None:
        results[content] = await scanner.scan(content, None, 0.75)

    # fail_after guards against a routing regression that wedges a caller forever.
    with anyio.fail_after(5):
        async with anyio.create_task_group() as tg:
            for content in contents:
                tg.start_soon(run, content)

    assert set(results) == set(contents)
    for content in contents:
        (detection,) = results[content]
        # The match echoes the submitted content; any mismatch is a crossed wire.
        assert detection.match == content
