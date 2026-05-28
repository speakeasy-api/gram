from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path


ROOT = Path(__file__).resolve().parent
CUSTOM_IMAGE = "gram-presidio-lab:local"
DEFAULT_BASELINE_URL = "http://127.0.0.1:5050"


@dataclass
class ServiceResult:
    name: str
    supported_entities: list[str] | None
    cases: list[dict[str, object]]

    @property
    def hits(self) -> int:
        return sum(1 for case in self.cases if case["matched"])

    @property
    def total(self) -> int:
        return len(self.cases)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--baseline-url", default=DEFAULT_BASELINE_URL)
    parser.add_argument("--skip-build", action="store_true")
    args = parser.parse_args()

    cases = json.loads((ROOT / "cases.json").read_text())

    if not args.skip_build:
        run(["docker", "build", "-t", CUSTOM_IMAGE, str(ROOT)])

    custom = Container(CUSTOM_IMAGE, 13002, "presidio-custom")

    try:
        custom.start()

        baseline_result = evaluate_service("baseline", args.baseline_url, cases)
        custom_result = evaluate_service("custom", custom.base_url, cases)
        emit_report(baseline_result, custom_result)
    finally:
        custom.stop()

    return 0


class Container:
    def __init__(self, image: str, port: int, name_prefix: str) -> None:
        self.image = image
        self.port = port
        self.name = f"{name_prefix}-{int(time.time())}"
        self.base_url = f"http://127.0.0.1:{port}"

    def start(self) -> None:
        run(["docker", "rm", "-f", self.name], check=False)
        run(
            [
                "docker",
                "run",
                "--rm",
                "-d",
                "--name",
                self.name,
                "-p",
                f"{self.port}:3000",
                self.image,
            ]
        )
        wait_for_health(self.base_url)

    def stop(self) -> None:
        run(["docker", "rm", "-f", self.name], check=False)


def evaluate_service(name: str, base_url: str, cases: list[dict[str, str]]) -> ServiceResult:
    supported_entities = fetch_supported_entities(base_url)
    results: list[dict[str, object]] = []
    for case in cases:
        payload = {
            "text": [case["text"]],
            "language": "en",
            "score_threshold": 0.0,
            "entities": [case["entity"]],
        }
        status_code, body = post_json(f"{base_url}/analyze", payload)
        parsed = None
        matched = False
        if status_code == 200:
            parsed = json.loads(body)
            findings = parsed[0] if parsed else []
            matched = any(finding.get("entity_type") == case["entity"] for finding in findings)
        results.append(
            {
                "id": case["id"],
                "entity": case["entity"],
                "status_code": status_code,
                "matched": matched,
                "body": parsed if parsed is not None else body,
            }
        )
    return ServiceResult(name=name, supported_entities=supported_entities, cases=results)


def emit_report(baseline: ServiceResult, custom: ServiceResult) -> None:
    print()
    print(f"baseline supported entities: {len(baseline.supported_entities or [])}")
    print(f"custom supported entities:   {len(custom.supported_entities or [])}")
    print()
    print("case                           entity                              baseline  custom")
    print("------------------------------------------------------------------------------------")
    custom_by_id = {case["id"]: case for case in custom.cases}
    for baseline_case in baseline.cases:
        custom_case = custom_by_id[baseline_case["id"]]
        print(
            f"{baseline_case['id']:<30} "
            f"{baseline_case['entity']:<35} "
            f"{format_case(baseline_case):<9} "
            f"{format_case(custom_case):<9}"
        )
    print("------------------------------------------------------------------------------------")
    print(f"coverage hits                    {baseline.hits}/{baseline.total}    {custom.hits}/{custom.total}")


def format_case(case: dict[str, object]) -> str:
    if case["matched"]:
        return "hit"
    return f"http{case['status_code']}"


def fetch_supported_entities(base_url: str) -> list[str] | None:
    try:
        with urllib.request.urlopen(f"{base_url}/supportedentities", timeout=5) as response:
            if response.status != 200:
                return None
            parsed = json.loads(response.read().decode("utf-8"))
            if isinstance(parsed, list):
                return [str(item) for item in parsed]
    except Exception:
        return None
    return None


def post_json(url: str, payload: dict[str, object]) -> tuple[int, str]:
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return response.status, response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8")


def wait_for_health(base_url: str) -> None:
    deadline = time.time() + 180
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(f"{base_url}/health", timeout=5) as response:
                if response.status == 200:
                    return
        except Exception:
            pass
        time.sleep(2)
    raise RuntimeError(f"timed out waiting for {base_url}/health")


def run(command: list[str], check: bool = True) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(command, text=True, capture_output=True, check=False)
    if check and result.returncode != 0:
        sys.stderr.write(result.stdout)
        sys.stderr.write(result.stderr)
        raise SystemExit(result.returncode)
    return result


if __name__ == "__main__":
    raise SystemExit(main())
