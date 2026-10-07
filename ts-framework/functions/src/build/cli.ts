import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { $ } from "zx";

/**
 * Environment variable that points at the CLI binary to deploy with. When set,
 * it wins over every other lookup.
 */
export const CLI_PATH_ENV = "SPEAKEASY_AI_CLI_PATH";

/** Deprecated name of {@link CLI_PATH_ENV}, read when that is unset. */
const LEGACY_CLI_PATH_ENV = "GRAM_CLI_PATH";

/**
 * Environment variable that selects the CLI built from this repository, and
 * its deprecated name, read when the new one is unset.
 */
const DEV_ENV = "SPEAKEASY_AI_DEV";
const LEGACY_DEV_ENV = "GRAM_DEV";

/**
 * Flag that makes the Speakeasy AI Control Plane CLI print
 * {@link CONTROL_PLANE_MARKER}. The Speakeasy SDK generator CLI installs a
 * binary with the same `speakeasy` name and rejects the flag.
 */
export const CONTROL_PLANE_MARKER_FLAG = "--control-plane-cli";

/** Fixed output of {@link CONTROL_PLANE_MARKER_FLAG}. */
export const CONTROL_PLANE_MARKER = "speakeasy-ai-control-plane-cli";

export const CLI_NOT_FOUND_MESSAGE = [
  "Speakeasy AI Control Plane CLI not found. Install it with one of:",
  "  brew install speakeasy-api/tap/cli",
  "  npm i -g @speakeasy-api/cli",
  `Or set ${CLI_PATH_ENV} to the path of the CLI binary.`,
].join("\n");

export type CLIResolverDeps = {
  /** Process environment. */
  env: Record<string, string | undefined>;

  /** Path of the CLI built from this repository, used when SPEAKEASY_AI_DEV is set. */
  localDevCLIPath: string;

  /** Reports whether a file exists at path. */
  fileExists: (path: string) => boolean;

  /** Reports whether command resolves on PATH. */
  commandExists: (command: string) => Promise<boolean>;

  /** Reports whether command is the Speakeasy AI Control Plane CLI. */
  isControlPlaneCLI: (command: string) => Promise<boolean>;
};

function isLocalDev(env: CLIResolverDeps["env"]): boolean {
  const value = env[DEV_ENV] ?? env[LEGACY_DEV_ENV];
  return value?.toLowerCase() === "true" || value === "1";
}

/**
 * Picks the CLI to deploy with, in this order:
 *
 * 1. The path in SPEAKEASY_AI_CLI_PATH, or the deprecated GRAM_CLI_PATH.
 * 2. The repository build at cli/bin/gram when SPEAKEASY_AI_DEV, or the
 *    deprecated GRAM_DEV, is set and the build exists.
 * 3. `speakeasy`, if it is the AI Control Plane CLI and not the SDK generator.
 * 4. The legacy `gram` command.
 *
 * Throws with install instructions when none is available.
 */
export async function resolveCLI(deps: CLIResolverDeps): Promise<string> {
  const override = (
    deps.env[CLI_PATH_ENV] ?? deps.env[LEGACY_CLI_PATH_ENV]
  )?.trim();
  if (override) {
    return override;
  }

  if (isLocalDev(deps.env) && deps.fileExists(deps.localDevCLIPath)) {
    return deps.localDevCLIPath;
  }

  if (await deps.isControlPlaneCLI("speakeasy")) {
    return "speakeasy";
  }

  if (await deps.commandExists("gram")) {
    return "gram";
  }

  throw new Error(CLI_NOT_FOUND_MESSAGE);
}

async function commandExists(command: string): Promise<boolean> {
  const lookup = process.platform === "win32" ? ["where"] : ["command", "-v"];
  const result = await $({ quiet: true, nothrow: true })`${lookup} ${command}`;
  return result.exitCode === 0;
}

async function isControlPlaneCLI(command: string): Promise<boolean> {
  if (!(await commandExists(command))) {
    return false;
  }

  const result = await $({
    quiet: true,
    nothrow: true,
    timeout: "10s",
  })`${command} ${CONTROL_PLANE_MARKER_FLAG}`;

  return (
    result.exitCode === 0 &&
    result.stdout
      .split("\n")
      .some((line) => line.trim() === CONTROL_PLANE_MARKER)
  );
}

export function defaultCLIResolverDeps(): CLIResolverDeps {
  return {
    env: process.env,
    // From ts-framework/functions/src/build -> ../../../../cli/bin/gram
    localDevCLIPath: resolve(
      dirname(new URL(import.meta.url).pathname),
      "../../../../cli/bin/gram",
    ),
    fileExists: existsSync,
    commandExists,
    isControlPlaneCLI,
  };
}
