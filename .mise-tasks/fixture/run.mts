#!/usr/bin/env node

//MISE description="Run the configured HTTPS fixture under Pitchfork"
//MISE dir="{{ config_root }}"
//MISE hide=true
//USAGE flag "--dry-run" help="Describe the daemon without executing it"

import { execFileSync, spawn } from "node:child_process";
import { isAbsolute } from "node:path";
import { dryRun, fail, readConfig, validateTLS } from "./lib.ts";

try {
  if (dryRun)
    console.log(
      "Would read private fixture bindings, validate TLS and run the pinned fixture on loopback with the local CA trusted for CIMD.",
    );
  else {
    const config = readConfig();
    validateTLS(config);
    const module = "github.com/speakeasy-api/benchmark-mcp-server";
    const pinnedTool = `go:${module}/cmd/benchmark-mcp-server@9c6d4f8c2e9828924bbafa7c249ad3eddc234606`;
    const managed = config["FIXTURE_BIN"] === "benchmark-mcp-server";
    const installEnvironment = { ...process.env };
    delete installEnvironment["FIXTURE_CONTROL_SECRET"];
    let executable = config["FIXTURE_BIN"];
    if (managed) {
      // Resolve the private pin with the caller's public trust, before giving
      // the direct fixture process its local CA and control secret.
      const inheritedPrivate = installEnvironment["GOPRIVATE"];
      for (const key of ["GOPRIVATE", "GONOPROXY", "GONOSUMDB"]) {
        const inherited = installEnvironment[key] ?? inheritedPrivate;
        installEnvironment[key] = [inherited, module].filter(Boolean).join(",");
      }
      try {
        execFileSync("mise", ["install", pinnedTool], {
          env: installEnvironment,
          stdio: "inherit",
        });
        executable = execFileSync(
          "mise",
          ["which", "--tool", pinnedTool, "benchmark-mcp-server"],
          { env: installEnvironment, encoding: "utf8" },
        ).trim();
      } catch {
        throw new Error(
          "Could not install or resolve the pinned fixture; check private Git access or supply --bin at fixture:start.",
        );
      }
      if (!isAbsolute(executable))
        throw new Error(
          "Pinned fixture did not resolve to an absolute executable.",
        );
    }
    const child = spawn(
      executable,
      [
        "--listen",
        `127.0.0.1:${config["FIXTURE_PORT"]}`,
        "--http-fixtures",
        "--control",
        "--fixture-base-url",
        config["FIXTURE_BASE_URL"],
        "--fixture-client-origins",
        config["FIXTURE_CLIENT_ORIGINS"],
        "--tls-cert",
        config["FIXTURE_TLS_CERT_FILE"],
        "--tls-key",
        config["FIXTURE_TLS_KEY_FILE"],
      ],
      {
        stdio: "inherit",
        env: {
          ...process.env,
          FIXTURE_CONTROL_SECRET: config["FIXTURE_CONTROL_SECRET"],
          SSL_CERT_FILE: config["FIXTURE_CA_FILE"],
        },
      },
    );
    let stopping = false;
    for (const signal of ["SIGINT", "SIGTERM"] as const)
      process.on(signal, () => {
        if (child.kill(signal)) stopping = true;
      });
    child.once("error", () =>
      fail(
        new Error(
          "Could not launch the fixture executable; check private Git access or supply --bin at fixture:start.",
        ),
      ),
    );
    child.once("exit", (code) => process.exit(code ?? (stopping ? 0 : 1)));
  }
} catch (error) {
  fail(error);
}
