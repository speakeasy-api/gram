import { existsSync } from "node:fs";
import {
  buildApplication,
  buildCommand,
  buildRouteMap,
  type CommandContext,
  type FlagParametersForType,
} from "@stricli/core";
import { getLogger, getLogLevels, type LogLevel } from "@logtape/logtape";

import pkg from "../../package.json" with { type: "json" };
import {
  findConfigFile,
  loadConfig,
  type ParsedUserConfig,
} from "../build/config.ts";
import { buildFunctions, deployFunction } from "../build/gram.ts";
import { configureLogger } from "../build/logging.ts";

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
      // A missing --config path falls back to the defaults, as it always has.
      const hit = configPath
        ? existsSync(configPath)
          ? configPath
          : undefined
        : findConfigFile(process.cwd());
      return loadConfig(hit);
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

function deprecationNotice(command: "build" | "push"): string {
  return `gf ${command} is deprecated and will be removed in a future release. Run \`speakeasy functions ${command}\` instead.`;
}

async function build(
  this: CommandContext,
  { "log-level": logLevel, config }: BuildFlags,
) {
  await configureLogger(logLevel);
  const logger = getLogger([pkg.name]);
  logger.warn(deprecationNotice("build"));

  await buildFunctions(logger, config);
}

async function push(
  this: CommandContext,
  { "log-level": logLevel, project, config }: PushFlags,
) {
  await configureLogger(logLevel);
  const logger = getLogger([pkg.name]);
  logger.warn(deprecationNotice("push"));

  if (project) {
    config.deployProject = project;
  }

  await deployFunction(logger, config);
}

export const app = buildApplication(routes, {
  name: "gf",
  versionInfo: {
    currentVersion: pkg.version,
  },
});
