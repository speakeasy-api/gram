from contextlib import asynccontextmanager
from types import SimpleNamespace
from typing import cast
from unittest.mock import Mock

import click
import pytest

from pystreams.cmd import multi as multi_mod
from pystreams.cmd.receiver import ReceiverGroup
from pystreams.risk.enforce_handler import PresidioEnforceHandler
from pystreams.risk.handler import PresidioHandler


class _StopAfterActivation(Exception):
    pass


class _Broker:
    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return None


async def test_blocking_detection_starts_after_scanner_initialization(
    monkeypatch: pytest.MonkeyPatch,
):
    startup_events: list[str] = []

    @asynccontextmanager
    async def fake_otel_sdk(*_args, **_kwargs):
        yield

    async def fake_publisher(*_args, **_kwargs):
        return object()

    async def fake_build_scanner(**_kwargs):
        startup_events.append("scanner initialized")
        return object()

    def fake_activate(*_args, **_kwargs):
        startup_events.append("blocking detection activated")
        raise _StopAfterActivation

    monkeypatch.setenv("GRAM_PYSTREAMS_DETECT_BLOCKING", "1")
    monkeypatch.setattr(multi_mod.logging, "configure_logging", lambda **_kwargs: None)
    monkeypatch.setattr(multi_mod.otel, "otel_sdk", fake_otel_sdk)
    monkeypatch.setattr(multi_mod, "_build_broker", lambda **_kwargs: _Broker())
    monkeypatch.setattr(multi_mod, "pubsub_publisher_for_message_async", fake_publisher)
    monkeypatch.setattr(multi_mod, "build_presidio_scanner", fake_build_scanner)
    monkeypatch.setattr(multi_mod, "activate_blocking_detection", fake_activate)

    with pytest.raises(_StopAfterActivation):
        await multi_mod.multi(
            service_version=None,
            environment="local",
            git_sha=None,
            log_level="INFO",
            pretty_log=False,
            enable_tracing=False,
            enable_metrics=False,
            role="analysis",
            gcp_project_id="test-project",
            pubsub_emulator_host=None,
            control_host="127.0.0.1",
            control_port=0,
            max_scan_concurrency=None,
            scan_workers=0,
            scan_max_tasks_per_child=1,
            scan_timeout=1,
            scan_slot_timeout=1,
            max_inflight=None,
            redis_addr=None,
            redis_password=None,
            risk_fingerprint_pepper_keyring=None,
        )

    assert startup_events == [
        "scanner initialized",
        "blocking detection activated",
    ]


class _Receivers:
    def __init__(self) -> None:
        self.subscriptions: list[type] = []

    async def receive(
        self, _message_type, subscription_type, _handler, **_kwargs
    ) -> None:
        self.subscriptions.append(subscription_type)


@pytest.mark.parametrize(
    ("role", "expected_subscriptions"),
    [
        (
            "analysis",
            [
                multi_mod.processor_pb2.PyProcessor,
                multi_mod.presidio_analyzer_pb2.PresidioAnalyzer,
            ],
        ),
        (
            "all",
            [
                multi_mod.processor_pb2.PyProcessor,
                multi_mod.presidio_analyzer_pb2.PresidioAnalyzer,
                multi_mod.presidio_enforcer_pb2.PresidioEnforcer,
            ],
        ),
        ("enforcement", [multi_mod.presidio_enforcer_pb2.PresidioEnforcer]),
    ],
)
async def test_role_registers_only_its_receivers(
    role: str, expected_subscriptions: list[type]
):
    receivers = _Receivers()
    handler = SimpleNamespace(handle=Mock())

    await multi_mod._register_receivers(
        role=role,
        receivers=cast(ReceiverGroup, receivers),
        logger=Mock(),  # type: ignore[arg-type]
        ping_log_level=10,
        presidio_handler=(
            cast(PresidioHandler, handler) if role in ("all", "analysis") else None
        ),
        enforce_handler=(
            cast(PresidioEnforceHandler, handler)
            if role in ("all", "enforcement")
            else None
        ),
        scan_workers=2,
        max_scan_concurrency=None,
        max_inflight=None,
    )

    assert receivers.subscriptions == expected_subscriptions


async def test_enforcement_role_requires_redis_and_pepper(
    monkeypatch: pytest.MonkeyPatch,
):
    monkeypatch.setattr(multi_mod.logging, "configure_logging", lambda **_kwargs: None)

    with pytest.raises(
        click.UsageError,
        match="required when --role=enforcement",
    ):
        await multi_mod.multi(
            service_version=None,
            environment="local",
            git_sha=None,
            log_level="INFO",
            pretty_log=False,
            enable_tracing=False,
            enable_metrics=False,
            role="enforcement",
            gcp_project_id="test-project",
            pubsub_emulator_host=None,
            control_host="127.0.0.1",
            control_port=0,
            max_scan_concurrency=None,
            scan_workers=0,
            scan_max_tasks_per_child=1,
            scan_timeout=1,
            scan_slot_timeout=1,
            max_inflight=None,
            redis_addr=None,
            redis_password=None,
            risk_fingerprint_pepper_keyring=None,
        )
