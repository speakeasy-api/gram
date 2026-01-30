#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Summarize development environment"
//MISE hide=true

import { $, chalk } from "zx";

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

async function pokePostgreSQL() {
  let result = await $`docker compose ps gram-db --format json`;
  if (!result.ok) {
    return console.log("⚪︎ Gram database: not running.");
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
  } catch (e: unknown) {
    return console.log(
      `⚪︎ Gram database: unable to get info from docker: ${e}`,
    );
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
    return console.log(
      "⚪︎ Gram database: container port 5432 does not appear to be published.",
    );
  }

  result =
    await $`docker compose exec gram-db psql -U ${process.env["DB_USER"]} -d ${process.env["DB_NAME"]} -c "SELECT 1"`;
  if (!result.ok) {
    return console.log(
      `⚪︎ Gram database: unable to connect to the database: ${result.stderr}`,
    );
  }

  console.log(
    chalk.greenBright(
      `⚫︎ Gram database is running on ${process.env["GRAM_DATABASE_URL"]}`,
    ),
  );
}

async function pokeOtel() {
  const result = await $`docker compose ps oteltui --format json`;
  if (!result.ok) {
    return console.log("⚪︎ otel-tui: not running.");
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
  } catch (e: unknown) {
    return console.log(`⚪︎ otel-tui: unable to get info from docker: ${e}`);
  }

  const portspec = parsed.Publishers.find((p) => {
    return (
      typeof p === "object" &&
      p &&
      "TargetPort" in p &&
      p.TargetPort === 4317 &&
      typeof p.PublishedPort === "number"
    );
  });

  const p =
    typeof portspec?.PublishedPort === "number" ? portspec.PublishedPort : null;

  if (p == null) {
    return console.log(
      "⚪︎ otel-tui: container port 4317 does not appear to be published.",
    );
  }

  console.log(chalk.greenBright(`⚫︎ otel-tui is running on port ${p}`));
}

await pokeOtel();
await pokePostgreSQL();
