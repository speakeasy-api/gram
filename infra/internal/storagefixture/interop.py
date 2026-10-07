"""Independent reader assertions: generated Go encoders never decode their output."""

import math
import sys
from pathlib import Path

import duckdb

root = Path(sys.argv[1])
db = duckdb.connect()


def rows(pattern: str, **options: bool) -> list[dict]:
    args = "".join(f", {name}={str(value).lower()}" for name, value in options.items())
    result = db.sql(
        f"SELECT * FROM read_parquet(?{args})", params=[str(root / pattern)]
    )
    return [dict(zip(result.columns, row, strict=True)) for row in result.fetchall()]


daily = rows("daily/**/*.parquet", hive_partitioning=True, union_by_name=True)
assert len(daily) == 6
pop = next(row for row in daily if row["id"] == "populated")
unset = next(
    row for row in daily if row["__pubsub"]["message_id"] == "0" and row["id"] is None
)
assert unset["child"] is None
assert unset["empty"] is None
assert unset["signed"] is None
assert unset["flag"] is None
assert unset["children"] == []
assert unset["attributes"] == {}
assert unset["empties"] == []
assert unset["implicit"] == 0
assert unset["__oneof_choice"] is None
assert pop["signed"] == -(2**63)
assert pop["unsigned"] == 2**64 - 1
assert pop["unsigned32"] == 2**32 - 1
assert pop["kind"] == 987
assert pop["zigzag32"] == pop["signed_fixed32"] == -(2**31)
assert pop["zigzag64"] == pop["signed_fixed64"] == -(2**63)
assert pop["fixed32"] == 2**32 - 1
assert pop["fixed64"] == 2**64 - 1
assert pop["flag"] is False
assert pop["data"] == b"\x00\xff"
assert math.isinf(pop["fraction"])
assert pop["small_fraction"] == -1.5
assert pop["child"]["value"] == ""
assert pop["child"]["numbers"] == [1, -2]
assert [item["numbers"] for item in pop["children"]] == [[3, 4], [], [5]]
assert pop["children"][1]["value"] is None
assert pop["attributes"]["a"]["numbers"] == [6, 7]
assert pop["attributes"]["z"]["value"] is None
assert pop["flags"] == {False: "no", True: "yes"}
assert pop["empty"]["__present"]
assert len(pop["empties"]) == 2
assert pop["number"] == 0
assert pop["__oneof_choice"] == 17
assert pop["new_field"] is None
assert pop["child"]["new_value"] is None
assert pop["empty"]["new_value"] is None
assert str(pop["part__year"]) == "2026"
assert str(pop["part__day"]) in {"7", "07"}
assert pop["__pubsub"]["topic"] == "fixture-v1-event"
assert pop["__pubsub"]["received_micros"] == 1791388800000000
assert any(row["__oneof_choice"] == 16 and row["text"] == "" for row in daily)
assert any(row["__oneof_choice"] == 19 and row["chosen_data"] == b"" for row in daily)
assert any(
    row["__oneof_choice"] == 18 and row["chosen_child"]["__present"] for row in daily
)
evolved = next(row for row in daily if row["id"] == "evolved")
assert evolved["child"]["new_value"] == "nested"
assert evolved["empty"]["new_value"] == "was-empty"
assert evolved["empties"][0]["new_value"] == "list"
assert evolved["new_field"] is False
assert evolved["__oneof_choice"] == 29
assert rows("hourly/**/*.parquet", hive_partitioning=True)[0]["part__hour"] == 14
external = rows(
    "external/**/*.parquet", hive_partitioning=True, hive_types_autocast=False
)[0]
assert external["region"] == "eu-west1"
assert external["account"] == "001"
opened = rows("open/*.parquet")
assert opened[0]["optional_value"] is None
assert opened[0]["implicit"] == 0
assert opened[1]["optional_value"] == 0
assert opened[1]["__oneof_choice"] == 4
assert opened[1]["text"] == ""
assert opened[1]["child"]["__present"]
