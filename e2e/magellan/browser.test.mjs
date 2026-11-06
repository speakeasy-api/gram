// Run with: node --test e2e/magellan/browser.test.mjs
import assert from "node:assert/strict";
import { test } from "node:test";

import { plan } from "./browser.mjs";

const config = {
  siteURL: "https://app.example.com",
  session: "magellan",
  outDir: "/work/out",
};

test("passes allowed commands through to the fixed session", () => {
  assert.deepEqual(plan(config, ["click", "e12"]), [
    "-s=magellan",
    "click",
    "e12",
  ]);
  assert.deepEqual(plan(config, ["fill", "e3", "Magellan 1", "--submit"]), [
    "-s=magellan",
    "fill",
    "e3",
    "Magellan 1",
    "--submit",
  ]);
});

test("refuses commands that read the login, run code or read files", () => {
  for (const cmd of [
    "eval",
    "run-code",
    "cookie-list",
    "cookie-get",
    "state-save",
    "state-load",
    "localstorage-list",
    "sessionstorage-get",
    "request-headers",
    "response-headers",
    "request",
    "route",
    "upload",
    "drop",
    "open",
    "close",
    "tracing-start",
    "pdf",
    "attach",
    "install",
  ]) {
    assert.throws(() => plan(config, [cmd]), /not allowed/, cmd);
  }
});

test("refuses options, including a second session", () => {
  assert.throws(() => plan(config, ["click", "e1", "-s=other"]), /option/);
  assert.throws(() => plan(config, ["goto", "--config=x"]), /option/);
  assert.throws(() => plan(config, ["fill", "e1", "x", "--submit=1"]), /value/);
});

test("keeps navigation on the site", () => {
  assert.deepEqual(plan(config, ["goto", "/org/projects/p"]), [
    "-s=magellan",
    "goto",
    "https://app.example.com/org/projects/p",
  ]);
  for (const url of [
    "https://evil.example/",
    "//evil.example/x",
    "https://app.example.com.evil.example/",
    "http://app.example.com/",
    "javascript:alert(1)",
    "file:///etc/passwd",
    "data:text/html,hi",
  ]) {
    assert.throws(() => plan(config, ["goto", url]), /only|URL/, url);
    assert.throws(() => plan(config, ["tab-new", url]), /only|URL/, url);
  }
});

test("writes screenshots only into the output directory", () => {
  assert.deepEqual(plan(config, ["screenshot", "--filename=bug-1.png"]), [
    "-s=magellan",
    "screenshot",
    "--filename=/work/out/bug-1.png",
  ]);
  for (const name of ["../x.png", "/etc/x.png", "a/b.png", ".x.png", "x.txt"]) {
    assert.throws(
      () => plan(config, ["screenshot", `--filename=${name}`]),
      /filename/,
      name,
    );
  }
});
