import assert from "node:assert/strict";
import { test } from "node:test";
import childProcess from "node:child_process";
import { syncBuiltinESMExports } from "node:module";
import { PassThrough } from "node:stream";
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

test("graph query requires a live provider in the server tenant before declaring list eligibility", async (t) => {
  let sql = "";
  const original = process.env.GRAM_DATABASE_URL;
  const stub = t.mock.method(childProcess, "execFile", (...args: unknown[]) => {
    const callback = args.at(-1) as (error: null, output: string) => void;
    const stdin = new PassThrough();
    stdin.on("data", (chunk) => {
      sql += chunk.toString();
    });
    stdin.on("finish", () =>
      callback(
        null,
        JSON.stringify({
          projects: ["project-1"],
          servers: [],
          gateways: [],
          automaticGateways: {
            organization: "not_captured",
            project: "not_captured",
          },
        }),
      ),
    );
    return { stdin } as unknown as ReturnType<typeof childProcess.execFile>;
  });
  syncBuiltinESMExports();
  process.env.GRAM_DATABASE_URL =
    "postgres://fixture:fixture@localhost/fixture";
  try {
    await captureAuthGraph([project]);
    const cte = sql.slice(
      sql.indexOf("gateway_registration_edges AS ("),
      sql.indexOf("SELECT json_build_object("),
    );
    assert.match(
      cte,
      /NOT c\.deleted AND i\.id IS NOT NULL AND NOT i\.deleted/,
    );
    assert.match(
      cte,
      /LEFT JOIN remote_session_issuers i ON i\.id = c\.remote_session_issuer_id\s+AND \(i\.project_id = s\.project_id\s+OR \(i\.project_id IS NULL AND \(i\.organization_id = s\.organization_id OR i\.organization_id IS NULL\)\)\)/,
    );
  } finally {
    stub.mock.restore();
    syncBuiltinESMExports();
    if (original === undefined) delete process.env.GRAM_DATABASE_URL;
    else process.env.GRAM_DATABASE_URL = original;
  }
});
