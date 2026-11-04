import assert from "node:assert/strict";
import { test } from "node:test";
import { captureAuthGraph } from "./graph.ts";

const project = "https://localhost:9443/fixture-org/projects/auth-e2e-example";

test("graph diagnostics reject unscoped or nonlocal projects before connecting", async () => {
  for (const urls of [
    [],
    Array(21).fill(project),
    [project.replace("localhost", "example.com")],
    [project.replace("auth-e2e-example", "unrelated-project")],
    [project + "?query=unexpected"],
    [project + "/settings"],
  ]) {
    await assert.rejects(captureAuthGraph(urls), {
      message: "AUTH_GRAPH_INVALID_PROJECTS",
    });
  }
});

test("graph diagnostics reject remote or unsafe database settings without exposing them", async () => {
  const original = process.env.GRAM_DATABASE_URL;
  try {
    for (const url of [
      "postgres://fixture:private-value@example.com/fixture",
      "postgres://fixture:private-value@localhost/fixture?options=-csearch_path=other",
      "postgres://fixture:private-value@localhost/fixture?search_path=public,other",
      "postgres://fixture:private-value@localhost/fixture?service=production",
      "not-a-database-url",
    ]) {
      process.env.GRAM_DATABASE_URL = url;
      await assert.rejects(captureAuthGraph([project]), {
        message: "AUTH_GRAPH_INVALID_DATABASE",
      });
    }
  } finally {
    if (original === undefined) delete process.env.GRAM_DATABASE_URL;
    else process.env.GRAM_DATABASE_URL = original;
  }
});

test("graph aliases reject an unsafe scope before connecting", async () => {
  await assert.rejects(captureAuthGraph([project], "unsafe' scope"), {
    message: "AUTH_GRAPH_INVALID_ALIAS_SCOPE",
  });
});
