"""The Speakeasy partition a process runs in.

A partition is an independent production deployment with its own data stores
and hosts. The set is closed and mirrors ``server/internal/partition`` in the Go
server: adding a partition is a code change in both, so a typo in configuration
fails at startup instead of silently naming a new partition.
"""

from typing import Final

US: Final = "us"
EU: Final = "eu"

# Every known partition, in the same order as ``partition.All`` in Go.
PARTITIONS: Final = (US, EU)

# The partition a process runs in when none is configured. Every deployment
# that predates partitions is the US one.
DEFAULT: Final = US
