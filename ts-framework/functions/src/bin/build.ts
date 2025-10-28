import {
  configure,
  getConsoleSink,
  getLogger,
  getLogLevels,
  type Logger,
  type LogLevel,
} from "@logtape/logtape";
import { getPrettyFormatter } from "@logtape/pretty";
import util from "node:util";

import { existsSync } from "node:fs";
import * as z from "zod";
import pkg from "../../package.json" with { type: "json" };
import { loadConfig } from "../build/config.ts";
import { buildFunctions } from "../build/gram.ts";

const usage = `
${pkg.name}/${pkg.version}

Usage: gram-build [options]

Options:
  -c, --config <path>      Path to the build configuration file
  -h, --help               Show this help message
  -v, --version            Show version information
      --log-level <level>  Set the log level (${getLogLevels().join(", ")})
`;

function parseArgs(argv: string[]) {
  return util.parseArgs({
    args: argv.slice(2),
    options: {
      help: { type: "boolean", short: "h" },
      version: { type: "boolean", short: "v" },
      config: { type: "string", short: "c" },
      "log-level": { type: "string" },
    },
  });
}

async function run(logger: Logger, args: ReturnType<typeof parseArgs>) {
  let configPath = args.values.config;
  if (!configPath) {
    configPath =
      [
        "gram.config.ts",
        "gram.config.mts",
        "gram.config.js",
        "gram.config.mjs",
      ].find((f) => existsSync(f)) || undefined;
  }

  const res = await loadConfig(configPath);
  if (!res.success) {
    logger.error("Invalid configuration", {
      error: z.prettifyError(res.error),
    });
    process.exit(1);
  }

  logger.debug("Config loaded", { ...res.data });

  await buildFunctions(logger, res.data);
}

async function main(argv: string[]) {
  console.log("HERE I AM");
  const { name, version } = pkg;
  const args = parseArgs(argv);
  if (args.values.help) {
    console.log(usage);
    return;
  }

  if (args.values.version) {
    console.log(`${name} ${version}`);
    return;
  }

  let lowestLevel = (args.values["log-level"] ||
    process.env["LOG_LEVEL"] ||
    "info") as LogLevel;
  if (!getLogLevels().includes(lowestLevel)) {
    lowestLevel = "info";
  }

  const isCI = process.env["CI"] === "true";

  await configure({
    sinks: {
      console: getConsoleSink({
        formatter: getPrettyFormatter({
          colors: !isCI,
          icons: !isCI,
          properties: true,
          timestampStyle: null,
          levelStyle: ["bold"],
          categoryStyle: ["italic"],
          messageStyle: null,
        }),
      }),
    },

    loggers: [
      {
        category: ["logtape", "meta"],
        lowestLevel: "warning",
        sinks: ["console"],
      },
      {
        category: ["gram", "build"],
        lowestLevel,
        sinks: ["console"],
      },
      {
        category: ["gram", "cli"],
        lowestLevel,
        sinks: ["console"],
      },
    ],
  });

  const logger = getLogger(["gram", "build"]);

  await run(logger, args).catch((err) => {
    logger.error("Build failed with error", {
      error: err instanceof Error ? err.message : String(err),
      stack: err instanceof Error && err.stack ? err.stack : undefined,
    });
    process.exit(1);
  });
}

if (import.meta.main) {
  await main(process.argv);
}
