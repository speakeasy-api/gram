"""Construct the configured PII scanner.

The ``multi`` command supports two scan strategies, selected by ``--scan-workers``:
a pool of worker processes (the default) or an in-process thread scanner. This
isolates that selection — and the analyzer/concurrency wiring each strategy needs
— behind one async factory so the command body stays about lifecycle, not
construction. ``GRAM_PII_BACKEND=clairic`` selects the hosted POC instead.
"""

from __future__ import annotations

import os

import structlog
from asyncer import asyncify

from pystreams.risk.scanner import (
    ProcessPoolScanner,
    Scanner,
    ThreadScanner,
    build_default_analyzer,
)


async def build_presidio_scanner(
    *,
    scan_workers: int,
    scan_max_tasks_per_child: int,
    scan_timeout: float,
    scan_slot_timeout: float,
    max_scan_concurrency: int | None,
    logger: structlog.stdlib.BoundLogger,
) -> Scanner:
    """Build the configured scan strategy.

    With ``scan_workers > 0`` (the default) the scan runs in a pool of worker
    processes, each with its own GIL, breaking the single-process throughput
    ceiling inside one pod. With ``scan_workers == 0`` it runs in-process on
    threads capped by ``max_scan_concurrency``. The pool owns its own per-worker
    spaCy models, so the in-process analyzer is only built for the threaded path.
    """
    backend = os.environ.get("GRAM_PII_BACKEND", "presidio")
    if backend == "clairic":
        from pystreams.risk.clairic import ClairicScanner

        logger.info("starting clairic PII scanner")
        # HTTP client setup reads the TLS trust store; keep it off the loop.
        return await asyncify(ClairicScanner.from_env)()
    if backend != "presidio":
        raise ValueError("GRAM_PII_BACKEND must be presidio or clairic")
    if scan_workers > 0:
        logger.info(
            "starting presidio scan pool",
            workers=scan_workers,
            max_tasks_per_child=scan_max_tasks_per_child,
            scan_timeout=scan_timeout,
            scan_slot_timeout=scan_slot_timeout,
        )
        return await ProcessPoolScanner.create(
            max_workers=scan_workers,
            max_tasks_per_child=scan_max_tasks_per_child,
            scan_timeout=scan_timeout,
            slot_timeout=scan_slot_timeout,
        )

    analyzer = await build_default_analyzer()
    # Concurrent Presidio scans are GIL-bound, so the scanner caps them at a low
    # default. ``max_scan_concurrency`` overrides it (<=0 disables the cap); unset
    # (None) leaves the scanner default in place.
    concurrency_kwargs = (
        {"max_concurrency": max_scan_concurrency}
        if max_scan_concurrency is not None
        else {}
    )
    return ThreadScanner(analyzer, **concurrency_kwargs)
