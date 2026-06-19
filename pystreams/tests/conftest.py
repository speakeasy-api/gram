"""Enforce event-loop blocking detection across the whole test suite.

Mirrors the runtime guard wired into ``cmd/multi.py`` (see
``pystreams.deps.blocking``) so blocking code in an async handler fails a test
instead of silently saturating the loop in production. Always fatal — there is no
warn-only mode here; CI and local behave identically.

aiocop works by patching the *running* event loop's scheduling methods, and
pytest-asyncio creates a fresh loop per test. So the global audit/slow-task setup
runs once per session, but the loop patch + raise-on-violations must be armed from
inside each test's own loop — hence the function-scoped async fixture.
"""

import aiocop
import pytest

from pystreams.deps.blocking import DEFAULT_THRESHOLD_MS


@pytest.fixture(scope="session", autouse=True)
def _configure_aiocop():
    """Register the audit hook and slow-task detection once for the session.

    These are loop-independent globals; ``detect_slow_tasks`` itself guards
    against being configured more than once.
    """
    aiocop.patch_audit_functions()
    aiocop.start_blocking_io_detection()
    aiocop.detect_slow_tasks(threshold_ms=DEFAULT_THRESHOLD_MS)
    yield


@pytest.fixture(autouse=True)
async def _enforce_no_blocking(_configure_aiocop):
    """Patch this test's running loop and raise on high-severity blocking IO.

    ``activate()`` runs aiocop's on-activate hooks against the loop that is live
    right now (idempotent per loop); ``enable_raise_on_violations`` arms the
    raise for this test's context. A high-severity blocking call then surfaces as
    a ``HighSeverityBlockingIoException`` that fails the test.
    """
    aiocop.activate()
    aiocop.enable_raise_on_violations()
    yield
