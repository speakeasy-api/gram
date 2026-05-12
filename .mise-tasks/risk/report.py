#!/usr/bin/env -S uv run --script
#MISE description="Run the prompt-injection risk report harness and print metrics"
#USAGE flag "--classifier-url <url>" help="Base URL for the L1 prompt-injection classifier sidecar. Also reads PI_CLASSIFIER_URL."
#USAGE flag "--metrics-file <path>" help="Path to the JSON metrics artifact. Defaults to server/risk_accuracy_metrics.json."
#USAGE flag "--no-run" help="Only print an existing metrics artifact without running the evaluator harness."
# /// script
# requires-python = ">=3.11"
# ///

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_METRICS_FILE = REPO_ROOT / "server" / "risk_accuracy_metrics.json"


def main() -> int:
    args = parse_args()
    metrics_file = args.metrics_file

    env = os.environ.copy()
    classifier_url = first_nonempty(args.classifier_url, env.get("PI_CLASSIFIER_URL"))

    if classifier_url:
        env["PI_CLASSIFIER_URL"] = classifier_url
    else:
        env.pop("PI_CLASSIFIER_URL", None)

    harness_exit = 0
    if not args.no_run:
        harness_exit = run_evaluator(env, metrics_file, classifier_url)

    if not metrics_file.exists():
        print(
            f"metrics file not found at {metrics_file}; the evaluator may have crashed before writing the artifact",
            file=sys.stderr,
        )
        return harness_exit or 1

    with metrics_file.open("r", encoding="utf-8") as fh:
        payload = json.load(fh)

    print_report(payload, classifier_url, metrics_file)
    return harness_exit


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Run the prompt-injection risk report harness and print L0/L1 opt-in metrics. "
            "Pass --classifier-url to compare production L1 opt-in behavior against L0."
        )
    )
    parser.add_argument(
        "--classifier-url",
        default=os.environ.get("usage_classifier_url"),
        help="Base URL for the L1 classifier sidecar, for example http://127.0.0.1:5051.",
    )
    parser.add_argument(
        "--metrics-file",
        type=Path,
        default=Path(os.environ.get("usage_metrics_file") or DEFAULT_METRICS_FILE),
        help=f"Path to metrics JSON. Default: {DEFAULT_METRICS_FILE}",
    )
    parser.add_argument(
        "--no-run",
        action="store_true",
        default=os.environ.get("usage_no_run") == "true",
        help="Print the existing metrics artifact without rerunning the evaluator harness.",
    )
    return parser.parse_args()


def run_evaluator(env: dict[str, str], metrics_file: Path, classifier_url: str | None) -> int:
    cmd = [
        "go",
        "run",
        "./server/cmd/risk-pi-report",
        "--out",
        str(metrics_file),
    ]
    if classifier_url:
        cmd.extend(["--classifier-url", classifier_url])
    return subprocess.run(cmd, cwd=REPO_ROOT, env=env, check=False).returncode


def print_report(payload: dict[str, Any], classifier_url: str | None, metrics_file: Path) -> None:
    summary = payload["summary"]

    print()
    print("Prompt-Injection Risk Report")
    print()
    print(f"ref:        {payload.get('ref', '-')}")
    print(f"git_sha:    {payload.get('git_sha', '-')}")
    print(f"timestamp:  {payload.get('timestamp', '-')}")
    print(f"artifact:   {metrics_file}")
    print(f"classifier: {classifier_status(classifier_url)}")
    print(f"corpus:     {corpus_status(summary)}")
    print()

    modes = summary.get("modes") or []
    if modes:
        print("Operational comparison:")
        print_table(
            ["mode", "status", "total", "tp", "fp", "tn", "fn", "precision", "recall", "f1", "accuracy", "fp_rate"],
            mode_rows(modes),
        )
        print()

        if has_modes(modes, "l0_default", "l1_opt_in"):
            print("Net change from enabling L1:")
            print_table(["metric", "change"], delta_rows(modes))
            print()

            print("Regression sources:")
            print_table(["source", "fp_before", "fp_after", "delta"], regression_source_rows(modes))
            print()

            print("Recall gain sources:")
            print_table(["source", "tp_before", "tp_after", "delta"], recall_gain_rows(modes))
            print()

        print("By source:")
        print_table(
            ["mode", "source", "tp", "fp", "tn", "fn", "recall", "fp_rate"],
            source_rows(modes),
        )
        print()

        print("By rule:")
        print_table(["mode", "rule_id", "tp", "fp"], rule_rows(modes))
        print()

        l1 = maybe_mode_by_name(modes, "l1_opt_in")
        if l1:
            print("New false positives:")
            print_table(["source", "id", "rule", "score", "text"], example_rows(l1.get("new_false_positives", [])))
            print()

            print("Recovered true positives:")
            print_table(["source", "id", "rule", "score", "text"], example_rows(l1.get("recovered_true_positives", [])))
            print()
        return

    print("L0 default:")
    print_counts(summary)
    print()

    print("By source:")
    print_table(
        ["source", "tp", "fp", "tn", "fn", "recall", "fp_rate"],
        [
            [
                item["source"],
                item["counts"]["tp"],
                item["counts"]["fp"],
                item["counts"]["tn"],
                item["counts"]["fn"],
                fmt(item["metrics"].get("recall")),
                fmt(item["metrics"].get("fp_rate")),
            ]
            for item in summary.get("by_source", [])
        ],
    )
    print()

    print("By rule:")
    print_table(
        ["rule_id", "tp", "fp"],
        [[item["rule_id"], item["tp_count"], item["fp_count"]] for item in summary.get("by_rule", [])],
    )
    print()


def print_counts(summary: dict[str, Any]) -> None:
    counts = summary["counts"]
    overall = summary["overall"]
    print(f"total:     {summary['total']}  (TP={counts['tp']} FP={counts['fp']} TN={counts['tn']} FN={counts['fn']})")
    print(f"precision: {fmt(overall.get('precision'))}")
    print(f"recall:    {fmt(overall.get('recall'))}")
    print(f"f1:        {fmt(overall.get('f1'))}")
    print(f"accuracy:  {fmt(overall.get('accuracy'))}")
    print(f"fp_rate:   {fmt(overall.get('fp_rate'))}")


def mode_rows(modes: list[dict[str, Any]]) -> list[list[Any]]:
    rows: list[list[Any]] = []
    for mode in modes:
        if mode.get("skipped"):
            rows.append(
                [
                    display_mode_name(mode["name"]),
                    f"skipped: {mode.get('skip_reason', '')}",
                    "-",
                    "-",
                    "-",
                    "-",
                    "-",
                    "-",
                    "-",
                    "-",
                    "-",
                    "-",
                ]
            )
            continue

        counts = mode["counts"]
        overall = mode["overall"]
        rows.append(
            [
                display_mode_name(mode["name"]),
                "ok",
                mode["total"],
                counts["tp"],
                counts["fp"],
                counts["tn"],
                counts["fn"],
                fmt(overall.get("precision")),
                fmt(overall.get("recall")),
                fmt(overall.get("f1")),
                fmt(overall.get("accuracy")),
                fmt(overall.get("fp_rate")),
            ]
        )
    return rows


def display_mode_name(name: str) -> str:
    if name == "l0_default":
        return "L0 only"
    if name == "l1_opt_in":
        return "L0 + L1 opt-in"
    return name


def source_rows(modes: list[dict[str, Any]]) -> list[list[Any]]:
    rows: list[list[Any]] = []
    for mode in modes:
        if mode.get("skipped"):
            continue
        for item in mode.get("by_source", []):
            counts = item["counts"]
            metrics = item["metrics"]
            rows.append(
                [
                    display_mode_name(mode["name"]),
                    item["source"],
                    counts["tp"],
                    counts["fp"],
                    counts["tn"],
                    counts["fn"],
                    fmt(metrics.get("recall")),
                    fmt(metrics.get("fp_rate")),
                ]
            )
    return rows


def corpus_status(summary: dict[str, Any]) -> str:
    counts = summary["counts"]
    malicious = counts["tp"] + counts["fn"]
    benign = counts["fp"] + counts["tn"]
    return f"{summary['total']} cases ({malicious} malicious, {benign} benign)"


def rule_rows(modes: list[dict[str, Any]]) -> list[list[Any]]:
    rows: list[list[Any]] = []
    for mode in modes:
        if mode.get("skipped"):
            continue
        for item in mode.get("by_rule", []):
            rows.append([display_mode_name(mode["name"]), item["rule_id"], item["tp_count"], item["fp_count"]])
    return rows


def delta_rows(modes: list[dict[str, Any]]) -> list[list[Any]]:
    l0 = mode_by_name(modes, "l0_default")
    l1 = mode_by_name(modes, "l1_opt_in")
    l0_counts = l0["counts"]
    l1_counts = l1["counts"]
    l0_overall = l0["overall"]
    l1_overall = l1["overall"]
    return [
        ["true positives", signed(l1_counts["tp"] - l0_counts["tp"])],
        ["false negatives", signed(l1_counts["fn"] - l0_counts["fn"])],
        ["false positives", signed(l1_counts["fp"] - l0_counts["fp"])],
        ["recall", signed_percentage_points(l1_overall["recall"] - l0_overall["recall"])],
        ["f1", signed_percentage_points(l1_overall["f1"] - l0_overall["f1"])],
        ["fp_rate", signed_percentage_points(l1_overall["fp_rate"] - l0_overall["fp_rate"])],
    ]


def regression_source_rows(modes: list[dict[str, Any]]) -> list[list[Any]]:
    return source_delta_rows(modes, "fp")


def recall_gain_rows(modes: list[dict[str, Any]]) -> list[list[Any]]:
    return source_delta_rows(modes, "tp")


def source_delta_rows(modes: list[dict[str, Any]], field: str) -> list[list[Any]]:
    l0 = mode_by_name(modes, "l0_default")
    l1 = mode_by_name(modes, "l1_opt_in")
    baseline = source_counts(l0)
    candidate = source_counts(l1)
    rows: list[list[Any]] = []
    for source in sorted(set(baseline) | set(candidate)):
        before = baseline.get(source, {}).get(field, 0)
        after = candidate.get(source, {}).get(field, 0)
        delta = after - before
        if delta <= 0:
            continue
        rows.append([source, before, after, signed(delta)])
    rows.sort(key=lambda row: int(str(row[3])), reverse=True)
    return rows


def source_counts(mode: dict[str, Any]) -> dict[str, dict[str, int]]:
    return {item["source"]: item["counts"] for item in mode.get("by_source", [])}


def example_rows(examples: list[dict[str, Any]]) -> list[list[Any]]:
    return [
        [
            item["source"],
            item["id"],
            item["rule_id"],
            fmt(item.get("score")),
            shorten(item["text"], 96),
        ]
        for item in examples
    ]


def print_table(headers: list[str], rows: list[list[Any]]) -> None:
    if not rows:
        print("(none)")
        return

    rendered = [[str(cell) for cell in row] for row in rows]
    widths = [
        max(len(headers[i]), *(len(row[i]) for row in rendered))
        for i in range(len(headers))
    ]
    print("  ".join(headers[i].ljust(widths[i]) for i in range(len(headers))))
    for row in rendered:
        print("  ".join(row[i].ljust(widths[i]) for i in range(len(headers))))


def classifier_status(classifier_url: str | None) -> str:
    if not classifier_url:
        return "disabled (pass --classifier-url or set PI_CLASSIFIER_URL to include L1 opt-in mode)"

    return f"enabled ({classifier_url})"


def has_modes(modes: list[dict[str, Any]], *names: str) -> bool:
    available = {mode["name"] for mode in modes if not mode.get("skipped")}
    return all(name in available for name in names)


def mode_by_name(modes: list[dict[str, Any]], name: str) -> dict[str, Any]:
    for mode in modes:
        if mode["name"] == name and not mode.get("skipped"):
            return mode
    raise KeyError(name)


def maybe_mode_by_name(modes: list[dict[str, Any]], name: str) -> dict[str, Any] | None:
    for mode in modes:
        if mode["name"] == name and not mode.get("skipped"):
            return mode
    return None


def first_nonempty(*values: str | None) -> str | None:
    for value in values:
        if value and value.strip():
            return value.strip()
    return None


def fmt(value: float | int | None) -> str:
    if value is None:
        return "-"
    return str(round(float(value), 4)).rstrip("0").rstrip(".")


def signed(value: int) -> str:
    return f"{value:+d}"


def signed_float(value: float) -> str:
    return f"{round(value, 4):+g}"


def signed_percentage_points(value: float) -> str:
    return f"{value * 100:+.1f}pp"


def shorten(value: str, limit: int) -> str:
    normalized = " ".join(value.split())
    if len(normalized) <= limit:
        return normalized
    return normalized[: limit - 3].rstrip() + "..."


if __name__ == "__main__":
    raise SystemExit(main())
