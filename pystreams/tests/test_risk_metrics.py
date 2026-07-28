import pytest

from pystreams.risk import metrics


@pytest.mark.parametrize(
    ("content_chars", "expected"),
    [
        (0, "0-1k"),
        (999, "0-1k"),
        (1_000, "1k-10k"),
        (9_999, "1k-10k"),
        (10_000, "10k-100k"),
        (99_999, "10k-100k"),
        (100_000, "100k-1m"),
        (999_999, "100k-1m"),
        (1_000_000, "1m-inf"),
        (50_000_000, "1m-inf"),
    ],
)
def test_size_bucket_boundaries(content_chars: int, expected: str):
    # Bounds are exclusive upper limits: a value on a boundary belongs to the
    # next band up. Labels must stay stable — they are metric tag values that
    # dashboards and monitors key on.
    assert metrics.size_bucket_for(content_chars) == expected
