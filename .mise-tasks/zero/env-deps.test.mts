import assert from "node:assert/strict";
import { test } from "node:test";
import { dependentEnvVars } from "./env-deps.mts";

test("finds direct and transitive dependents, in order", () => {
  const out = dependentEnvVars(
    {
      HOST: "localhost",
      C: "{{env.B}}/c",
      URL: "https://{{env.HOST}}",
      B: "{{env.URL}}/b",
      OTHER: "x",
    },
    ["HOST"],
  );
  assert.deepEqual(out, [
    ["URL", "https://{{env.HOST}}"],
    ["B", "{{env.URL}}/b"],
    ["C", "{{env.B}}/c"],
  ]);
});

test("reads an inline { default } template, as parseTOML returns it", () => {
  const out = dependentEnvVars(
    { HOST: "localhost", URL: { default: "https://{{env.HOST}}" } },
    ["HOST"],
  );
  assert.deepEqual(out, [["URL", "https://{{env.HOST}}"]]);
});

test("a { default } template's dependents follow it", () => {
  const out = dependentEnvVars(
    {
      HOST: "localhost",
      URL: { default: "https://{{env.HOST}}" },
      API: "{{env.URL}}/api",
    },
    ["HOST"],
  );
  assert.deepEqual(
    out.map(([k]) => k),
    ["URL", "API"],
  );
});

test("fails on another value shape that reads a root", () => {
  assert.throws(
    () =>
      dependentEnvVars(
        { HOST: "x", URL: { value: "{{env.HOST}}", redact: true } },
        ["HOST"],
      ),
    /URL reads HOST but is not a string or \{ default = "\.\.\." \}/,
  );
});

test("fails on another value shape that reads a dependent", () => {
  assert.throws(
    () =>
      dependentEnvVars(
        { HOST: "x", URL: "{{env.HOST}}", API: { value: "{{env.URL}}" } },
        ["HOST"],
      ),
    /API reads URL/,
  );
});

test("ignores other value shapes that read nothing affected", () => {
  assert.deepEqual(
    dependentEnvVars(
      {
        HOST: "x",
        N: 1,
        T: true,
        D: { default: "unset" },
        O: { value: "{{env.ELSE}}" },
      },
      ["HOST"],
    ),
    [],
  );
});

test("rejects a cycle", () => {
  assert.throws(
    () =>
      dependentEnvVars(
        { HOST: "x", A: "{{env.B}}{{env.HOST}}", B: "{{env.A}}" },
        ["HOST"],
      ),
    /Cyclic environment dependency/,
  );
});

test("leaves the roots out", () => {
  assert.deepEqual(
    dependentEnvVars({ HOST: "{{env.BASE}}", BASE: "b" }, ["HOST"]),
    [],
  );
});
