"""Exercise both dashboard NGINX configs: python3 client/test_nginx_logging.py.

Requires Docker. Uses the same base image as the dashboard Dockerfiles, without
requiring a frontend build or installing Python dependencies.
"""

import os
import subprocess
import tempfile
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path

CLIENT = Path(__file__).resolve().parent
IMAGE = os.environ.get("NGINX_TEST_IMAGE", "nginxinc/nginx-unprivileged:alpine")


def docker(*args):
    return subprocess.check_output(
        ["docker", *args], text=True, stderr=subprocess.STDOUT
    ).strip()


class DashboardAccessLoggingTest(unittest.TestCase):
    def test_only_successful_root_probes_are_suppressed(self):
        for app in ("dashboard", "admin"):
            with self.subTest(app=app), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                (root / "index.html").write_text("<!doctype html><title>ready</title>")
                # Exercise the dashboard's real entrypoint/envsubst path; the
                # admin image loads its configuration directly instead.
                (root / "default.conf").write_text(
                    (CLIENT / app / "nginx.conf").read_text()
                )
                destination = (
                    "/etc/nginx/templates/default.conf.template"
                    if app == "dashboard"
                    else "/etc/nginx/conf.d/default.conf"
                )
                container = docker(
                    "run",
                    "-d",
                    "-p",
                    "127.0.0.1::3000",
                    "-e",
                    "GRAM_CDN_URL=",
                    "-v",
                    f"{root / 'default.conf'}:{destination}:ro",
                    "-v",
                    f"{root}:/usr/share/nginx/html:ro",
                    IMAGE,
                )
                try:
                    port = docker("port", container, "3000/tcp").split(":")[-1]
                    base = f"http://127.0.0.1:{port}"

                    def request(path, agent, method="GET", base=base):
                        req = urllib.request.Request(
                            base + path, headers={"User-Agent": agent}, method=method
                        )
                        try:
                            with urllib.request.urlopen(req, timeout=3) as response:
                                return response.status
                        except urllib.error.HTTPError as error:
                            return error.code

                    for attempt in range(50):
                        try:
                            request("/?startup", "logging-test")
                            break
                        except OSError:
                            if attempt == 49:
                                raise
                            time.sleep(0.1)

                    self.assertEqual(request("/", "kube-probe/1.35"), 200)
                    self.assertEqual(request("/", "ordinary-browser"), 200)
                    self.assertEqual(request("/?probe=1", "kube-probe/1.35"), 200)
                    self.assertEqual(request("/missing.js", "kube-probe/1.35"), 404)
                    self.assertEqual(request("/", "kube-probe/1.35", "HEAD"), 200)
                    self.assertEqual(request("/", "not-kube-probe/1.35"), 200)
                    (root / "index.html").unlink()
                    self.assertGreaterEqual(request("/", "kube-probe/1.35"), 400)
                    # Stop gracefully so all access-log writes are flushed.
                    docker("stop", container)
                    lines = docker("logs", container).splitlines()
                    successful_probes = [
                        line
                        for line in lines
                        if '"GET / HTTP/1.1" 200' in line
                        and '"kube-probe/1.35"' in line
                    ]
                    self.assertEqual(
                        successful_probes, [], "successful probe was logged"
                    )
                    for marker in (
                        '"ordinary-browser"',
                        '"GET /?probe=1 HTTP/1.1"',
                        '"GET /missing.js HTTP/1.1" 404',
                        '"HEAD / HTTP/1.1" 200',
                        '"not-kube-probe/1.35"',
                    ):
                        self.assertTrue(any(marker in line for line in lines), marker)
                    self.assertTrue(
                        any(
                            '"GET / HTTP/1.1"' in line
                            and '"kube-probe/1.35"' in line
                            and '"GET / HTTP/1.1" 200' not in line
                            for line in lines
                        ),
                        "failed root probe must remain logged",
                    )
                except BaseException:
                    subprocess.run(["docker", "logs", container], check=False)
                    raise
                finally:
                    subprocess.run(
                        ["docker", "rm", "-f", container],
                        check=True,
                        stdout=subprocess.DEVNULL,
                    )


if __name__ == "__main__":
    unittest.main()
