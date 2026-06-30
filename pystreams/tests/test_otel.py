import structlog
from opentelemetry.sdk.metrics import (
    Counter,
    Histogram,
    ObservableCounter,
    ObservableGauge,
    ObservableUpDownCounter,
    UpDownCounter,
)
from opentelemetry.sdk.metrics.export import AggregationTemporality

from pystreams import attr
from pystreams.deps import otel

_LOGGER = structlog.get_logger()


def _options(**overrides) -> otel.OTelOptions:
    base = {
        "service_name": "gram-pystreams",
        "service_version": "1.2.3",
        "git_sha": "abc123",
        "environment": "test",
        "enable_tracing": False,
        "enable_metrics": False,
    }
    base.update(overrides)
    return otel.OTelOptions(**base)


def test_delta_temporality_matches_go_selector():
    # Delta everywhere except the non-monotonic UpDownCounters, which stay
    # cumulative.
    temporality = otel._delta_temporality()

    delta = AggregationTemporality.DELTA
    cumulative = AggregationTemporality.CUMULATIVE

    assert temporality[Counter] == delta
    assert temporality[Histogram] == delta
    assert temporality[ObservableCounter] == delta
    assert temporality[ObservableGauge] == delta
    assert temporality[UpDownCounter] == cumulative
    assert temporality[ObservableUpDownCounter] == cumulative


def test_build_resource_includes_git_and_service_attrs():
    resource = otel._build_resource(_options())

    assert resource.attributes[attr.SERVICE_NAME] == "gram-pystreams"
    assert resource.attributes[attr.SERVICE_VERSION] == "1.2.3"
    assert resource.attributes[attr.SERVICE_ENVIRONMENT] == "test"
    assert resource.attributes[attr.DATADOG_GIT_COMMIT_SHA] == "abc123"
    assert (
        resource.attributes[attr.DATADOG_GIT_REPOSITORY_URL]
        == "github.com/speakeasy-api/gram"
    )


def test_build_resource_drops_unset_optional_attrs():
    # Unset version/env/sha must not surface as empty strings on the resource.
    resource = otel._build_resource(
        _options(service_version=None, environment=None, git_sha=None)
    )

    assert attr.SERVICE_VERSION not in resource.attributes
    assert attr.SERVICE_ENVIRONMENT not in resource.attributes
    assert attr.DATADOG_GIT_COMMIT_SHA not in resource.attributes
    # The repo URL is a constant and always present.
    assert attr.DATADOG_GIT_REPOSITORY_URL in resource.attributes


def test_install_disabled_registers_no_providers():
    # With both signals off, no SDK provider is installed (the API keeps its
    # no-ops) so there is nothing to shut down.
    shutdowns = otel._install(_options(), _LOGGER)

    assert shutdowns == []


def test_shutdown_survives_a_failing_provider():
    calls: list[str] = []

    def boom() -> None:
        raise RuntimeError("flush failed")

    def ok() -> None:
        calls.append("ok")

    # A raising shutdown must not strand the others.
    otel._shutdown([boom, ok], _LOGGER)

    assert calls == ["ok"]


async def test_otel_sdk_disabled_is_a_clean_no_op():
    async with otel.otel_sdk(_options(), logger=_LOGGER):
        pass
