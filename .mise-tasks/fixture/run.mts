#!/usr/bin/env node

//MISE description="Run the configured HTTPS fixture under Pitchfork"
//MISE dir="{{ config_root }}"
//MISE hide=true
//USAGE flag "--dry-run" help="Describe the daemon without executing it"

import { spawn } from "node:child_process";
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
    const pinnedTool = `go:${module}/cmd/benchmark-mcp-server@7cec927eaab8348c0699c6faf547843e3bf9f4ae`;
    const managed = config["FIXTURE_BIN"] === "benchmark-mcp-server";
    const environment = { ...process.env };
    if (managed) {
      // Keep private Go module resolution local to this explicit fixture launch.
      const inheritedPrivate = environment["GOPRIVATE"];
      for (const key of ["GOPRIVATE", "GONOPROXY", "GONOSUMDB"]) {
        const inherited = environment[key] ?? inheritedPrivate;
        environment[key] = [inherited, module].filter(Boolean).join(",");
      }
    }
    const child = spawn(
      managed ? "mise" : config["FIXTURE_BIN"],
      [
        ...(managed ? ["exec", pinnedTool, "--", "benchmark-mcp-server"] : []),
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
          ...environment,
          FIXTURE_CONTROL_SECRET: config["FIXTURE_CONTROL_SECRET"],
          SSL_CERT_FILE: config["FIXTURE_CA_FILE"],
        },
      },
    );
    for (const signal of ["SIGINT", "SIGTERM"] as const)
      process.on(signal, () => child.kill(signal));
    child.once("error", () =>
      fail(
        new Error(
          "Could not launch the fixture executable; check private Git access or supply --bin at fixture:start.",
        ),
      ),
    );
    child.once("exit", (code, signal) =>
      process.exit(signal ? 0 : (code ?? 1)),
    );
  }
} catch (error) {
  fail(error);
}
