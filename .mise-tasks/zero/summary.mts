#!/usr/bin/env -S node

//MISE description="Summarize development environment"
//MISE alias="info"
//MISE hide=true

import { $, chalk, tmpfile } from "zx";

$.verbose = false;

const trueish = new Set(["true", "1", "yes", "on"]);

if (trueish.has(process.env["GRAM_SINGLE_PROCESS"] ?? "")) {
  console.log(
    chalk.greenBright(
      "⚫︎ Server and worker will run as a single process (GRAM_SINGLE_PROCESS)",
    ),
  );
}

if (trueish.has(process.env["GRAM_LOG_PRETTY"] ?? "")) {
  console.log(
    chalk.greenBright("⚫︎ Pretty logging is enabled (GRAM_LOG_PRETTY)"),
  );
} else {
  console.log("⚪︎ Pretty logging is disabled (GRAM_LOG_PRETTY)");
}

if (trueish.has(process.env["GRAM_ENABLE_OTEL_TRACES"] ?? "")) {
  console.log(
    chalk.greenBright(
      "⚫︎ OpenTelemetry traces are enabled (GRAM_ENABLE_OTEL_TRACES)",
    ),
  );
} else {
  console.log("⚪︎ OpenTelemetry traces are disabled (GRAM_ENABLE_OTEL_TRACES)");
}

if (trueish.has(process.env["GRAM_ENABLE_OTEL_METRICS"] ?? "")) {
  console.log(
    chalk.greenBright(
      "⚫︎ OpenTelemetry metrics are enabled (GRAM_ENABLE_OTEL_METRICS)",
    ),
  );
} else {
  console.log(
    "⚪︎ OpenTelemetry metrics are disabled (GRAM_ENABLE_OTEL_METRICS)",
  );
}

const assistantRuntimeHostKind =
  process.env["GRAM_ASSISTANT_RUNTIME_HOST_KIND"] ?? "";
if (assistantRuntimeHostKind) {
  console.log(
    chalk.greenBright(
      `⚫︎ Assistant runtime host kind is ${assistantRuntimeHostKind} (GRAM_ASSISTANT_RUNTIME_HOST_KIND)`,
    ),
  );
}

const assistantRuntimeServerURL =
  process.env["GRAM_ASSISTANT_RUNTIME_SERVER_URL"] ?? "";
if (assistantRuntimeServerURL) {
  console.log(
    chalk.greenBright(
      `⚫︎ Assistant runtimes will reach Gram via ${assistantRuntimeServerURL}`,
    ),
  );
}

const tableRows: [string, boolean, string][] = [];

function row(name: string, running: boolean, detail: string) {
  tableRows.push([name, running, detail]);
}

async function pokePostgreSQL() {
  const dbURL = process.env["GRAM_DATABASE_URL"] ?? "postgres://localhost/gram";
  let result = await $`docker compose ps gram-db --format json`.nothrow();
  if (!result.ok) {
    return row("Database", false, dbURL);
  }

  let parsed: unknown = {};
  try {
    parsed = JSON.parse(result.stdout);
    if (
      typeof parsed !== "object" ||
      !parsed ||
      !("Publishers" in parsed) ||
      !Array.isArray(parsed.Publishers)
    ) {
      throw new Error("Unexpected container info");
    }
  } catch {
    return row("Database", false, dbURL);
  }

  const portspec = parsed.Publishers.find((p) => {
    return (
      typeof p === "object" &&
      p &&
      "TargetPort" in p &&
      p.TargetPort === 5432 &&
      typeof p.PublishedPort === "number"
    );
  });

  const p =
    typeof portspec?.PublishedPort === "number" ? portspec.PublishedPort : null;

  if (p == null) {
    return row("Database", false, dbURL);
  }

  result =
    await $`docker compose exec -T gram-db psql -U ${process.env["DB_USER"]} -d ${process.env["DB_NAME"]} -c "SELECT 1"`.nothrow();
  if (!result.ok) {
    return row("Database", false, dbURL);
  }

  row("Database", true, dbURL);
}

await pokePostgreSQL();

async function pokeDockerService(
  serviceName: string,
  displayName: string,
  url: string,
) {
  let result =
    await $`docker compose ps ${serviceName} --format json`.nothrow();
  if (!result.ok) {
    return row(displayName, false, url);
  }

  let parsed: unknown = {};
  try {
    parsed = JSON.parse(result.stdout);
    if (typeof parsed !== "object" || !parsed || !("State" in parsed)) {
      throw new Error("Unexpected container info");
    }
  } catch {
    return row(displayName, false, url);
  }

  const state = (parsed as Record<string, unknown>).State;
  row(displayName, state === "running", url);
}

async function pokeHTTPService(
  displayName: string,
  healthURL: string,
  displayURL: string,
) {
  try {
    const result =
      await $`curl -sk -o /dev/null -w "%{http_code}" --connect-timeout 2 ${healthURL}`;
    const code = parseInt(result.stdout.trim(), 10);
    row(displayName, code >= 200 && code < 500, displayURL);
  } catch {
    row(displayName, false, displayURL);
  }
}

const temporalWebPort = process.env["TEMPORAL_WEB_PORT"] ?? "8233";
await pokeDockerService(
  "gram-temporal",
  "Temporal",
  `http://localhost:${temporalWebPort}`,
);

const jaegerWebPort = process.env["JAEGER_WEB_PORT"] ?? "16686";
await pokeDockerService(
  "jaeger",
  "Jaeger",
  `http://localhost:${jaegerWebPort}`,
);

const clickhouseHTTPPort = process.env["CLICKHOUSE_HTTP_PORT"] ?? "8123";
await pokeDockerService(
  "clickhouse",
  "ClickHouse",
  `http://localhost:${clickhouseHTTPPort}`,
);

const devIdpPort = process.env["GRAM_DEVIDP_PORT"] ?? "35291";
const devIdpURL = `http://localhost:${devIdpPort}`;
await pokeHTTPService("Mock IdP server", `${devIdpURL}/healthz`, devIdpURL);

const devIdpDashboardPort =
  process.env["GRAM_DEVIDP_DASHBOARD_PORT"] ?? "35293";
const devIdpDashboardURL = `http://localhost:${devIdpDashboardPort}`;
await pokeHTTPService(
  "Mock IdP dashboard",
  devIdpDashboardURL,
  devIdpDashboardURL,
);

const gramControlPort = process.env["GRAM_CONTROL_PORT"] ?? "8081";
const gramServerURL =
  process.env["GRAM_SERVER_URL"] ??
  `https://localhost:${process.env["GRAM_SERVER_PORT"] ?? "8080"}`;
await pokeHTTPService(
  "Gram server",
  `http://localhost:${gramControlPort}/healthz`,
  gramServerURL,
);

const gramHost = process.env["GRAM_HOST"] ?? "localhost";
const gramSitePort = process.env["GRAM_SITE_PORT"] ?? "5173";
const gramDashboardURL = `https://${gramHost}:${gramSitePort}`;
await pokeHTTPService("Gram dashboard", gramDashboardURL, gramDashboardURL);

tableRows.sort(([nameA, runningA], [nameB, runningB]) => {
  if (runningA !== runningB) return runningA ? -1 : 1;
  return nameA.localeCompare(nameB);
});

const q = (s: string) => `"${s}"`;
const csv = [
  ["Service", "Status", "Address"].map(q).join(","),
  ["", "", ""].join(","), // gum has a bug where first row is weirdly styled
  ...tableRows.map(([name, running, detail]) => {
    const status = running
      ? chalk.greenBright("RUNNING")
      : chalk.yellow("STOPPED");
    return [name, status, detail].map(q).join(",");
  }),
].join("\n");

const csvFile = tmpfile("services.csv", csv);
process.stdout.write("\n");
await $({
  stdio: "inherit",
})`gum table --print --selected.foreground="" --file ${csvFile}`;
