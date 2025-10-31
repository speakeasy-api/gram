import { existsSync } from "node:fs";
import {
  buildApplication,
  buildCommand,
  buildRouteMap,
  type CommandContext,
  type FlagParametersForType,
} from "@stricli/core";
import {
  configure,
  getConsoleSink,
  getLogger,
  getLogLevels,
  type LogLevel,
} from "@logtape/logtape";

import pkg from "../../package.json" with { type: "json" };
import { isCI, loadConfig, type ParsedUserConfig } from "../build/config.ts";
import { getPrettyFormatter } from "@logtape/pretty";
import { buildFunctions, deployFunction } from "../build/gram.ts";

interface SharedFlags {
  "log-level": LogLevel;
  config: ParsedUserConfig;
}

const sharedFlags: FlagParametersForType<SharedFlags, CommandContext> = {
  "log-level": {
    kind: "enum",
    values: getLogLevels(),
    default: "info",
    brief: "Logging severity level",
  },
  config: {
    kind: "parsed",
    optional: false,
    default: "",
    brief: "Path to the configuration file",
    parse: async (configPath: string) => {
      const candidates = configPath
        ? [configPath]
        : [
            "gram.config.ts",
            "gram.config.mts",
            "gram.config.js",
            "gram.config.mjs",
          ];

      const hit = candidates.find((f) => existsSync(f));
      const res = await loadConfig(hit);
      if (!res.success) {
        throw res.error;
      }
      return res.data;
    },
  },
};

interface BuildFlags extends SharedFlags {}
interface PushFlags extends SharedFlags {
  project?: string;
}

const routes = buildRouteMap({
  routes: {
    build: buildCommand({
      docs: {
        brief: "Build the project",
      },
      parameters: {
        flags: {
          ...sharedFlags,
        },
      },
      func: build,
    }),
    push: buildCommand({
      docs: {
        brief: "Push a new deployment using a built Gram Function",
      },
      parameters: {
        flags: {
          ...sharedFlags,
          project: {
            kind: "parsed",
            parse: String,
            optional: true,
            brief: "The Gram project to deploy to",
          },
        },
      },
      func: push,
    }),
  },
  docs: {
    brief: "Build and deploy Gram Functions",
  },
});

async function build(
  this: CommandContext,
  { "log-level": logLevel, config }: BuildFlags,
) {
  await configureLogger(logLevel);
  const logger = getLogger([pkg.name]);

  await buildFunctions(logger, config);
}

async function push(
  this: CommandContext,
  { "log-level": logLevel, project, config }: PushFlags,
) {
  await configureLogger(logLevel);
  const logger = getLogger([pkg.name]);

  if (project) {
    config.deployProject = project;
  }

  await deployFunction(logger, config);
}

async function configureLogger(lowestLevel: LogLevel) {
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
        category: [pkg.name],
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
}

export const app = buildApplication(routes, {
  name: "gf",
  versionInfo: {
    currentVersion: pkg.version,
  },
});
