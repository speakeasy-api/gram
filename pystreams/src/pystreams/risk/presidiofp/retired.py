"""Presidio recognizers whose every finding is treated as a false positive.

Mirror of ``server/internal/risk/presidiofp/retired.go``.
"""

# Entity types whose recognizer produces noise at a rate that makes every one of
# its findings untrustworthy, so the scanners drop them outright rather than
# filtering value by value.
#
# The live scanners already refuse these before this module is consulted (see
# ``_FINDING_LEVEL_DROP`` in ``pystreams.risk.scanner`` and
# ``findingLevelDropEntities`` in the Go risk_analysis package). Listing them
# here as well is what lets the offline sweep reconcile history with that
# decision: findings stored before the live drop landed are still sitting in
# risk_results, and a value-only catalog entry is what marks them as false
# positives.
RETIRED_RECOGNIZERS: dict[str, str] = {
    # Its patterns match a leading letter followed by a run of digits — the
    # shape of Figma file and node ids, short object ids, and countless other
    # opaque identifiers — at a score that any nearby "id", "number", "card" or
    # "lic" lemma (including the ones inside "public" and "duplicate") lifts
    # over the reporting threshold. Upstream has known about it since 2023:
    # microsoft/presidio#1063.
    "US_DRIVER_LICENSE": (
        "US driver license recognizer retired: matches arbitrary "
        "letter-and-digit identifiers"
    ),
}


def retired_reason(entity_type: str) -> str:
    """Return the retirement reason for an entity type, or "" when the
    recognizer is still trusted.
    """
    return RETIRED_RECOGNIZERS.get(entity_type, "")
