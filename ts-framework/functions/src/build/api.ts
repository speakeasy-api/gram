import { getConfig, getLogger, type LogLevel } from "@logtape/logtape";
import { resolve } from "node:path";
import pkg from "../../package.json" with { type: "json" };
import { findConfigFile, loadConfig, type ParsedUserConfig } from "./config.ts";
import { buildFunctions, inferSlug, resolveArtifacts } from "./gram.ts";
import { configureLogger } from "./logging.ts";

/**
 * Options for {@link resolveProject} and {@link build}. Relative paths are
 * resolved against `cwd`.
 */
export type ProjectOptions = {
  /** The project directory. Defaults to the process working directory. */
  cwd?: string | undefined;
  /** The config file. Defaults to the first `gram.config.*` file in `cwd`. */
  configFile?: string | undefined;
  /** Overrides the entrypoint set in the config file. */
  entrypoint?: string | undefined;
  /** Overrides the output directory set in the config file. */
  outDir?: string | undefined;
};

export type BuildOptions = ProjectOptions & {
  /** The lowest log level to print. Defaults to `info`. */
  logLevel?: LogLevel | undefined;
};

/** A project's resolved build and deploy settings. Paths are absolute. */
export type ResolvedProject = {
  cwd: string;
  outDir: string;
  zipFile: string;
  deployStagingFile: string;
  /** Unset when neither the config nor package.json gives a slug. */
  slug?: string | undefined;
  deployProject?: string | undefined;
  scale?: number | undefined;
  memoryMiB?: number | undefined;
};

export type BuildResult = {
  project: ResolvedProject;
  files: Array<{ path: string; size: number }>;
};

async function loadProjectConfig(
  opts: ProjectOptions,
): Promise<{ cwd: string; config: ParsedUserConfig }> {
  const cwd = resolve(opts.cwd ?? process.cwd());
  const configFile = opts.configFile
    ? resolve(cwd, opts.configFile)
    : findConfigFile(cwd);

  const res = await loadConfig(configFile);
  if (!res.success) {
    throw res.error;
  }

  const config = res.data;
  return {
    cwd,
    config: {
      ...config,
      cwd: resolve(cwd, config.cwd),
      // Like the other path options, an entrypoint override is relative to cwd.
      entrypoint: opts.entrypoint
        ? resolve(cwd, opts.entrypoint)
        : config.entrypoint,
      outDir: resolve(cwd, opts.outDir ?? config.outDir),
      deployStagingFile: resolve(cwd, config.deployStagingFile),
    },
  };
}

async function describeProject(
  cwd: string,
  config: ParsedUserConfig,
): Promise<ResolvedProject> {
  const { zipFilename } = await resolveArtifacts(config);

  return {
    cwd,
    outDir: config.outDir,
    zipFile: zipFilename,
    deployStagingFile: config.deployStagingFile,
    slug: config.slug || (await inferSlug(config.cwd).catch(() => undefined)),
    deployProject: config.deployProject,
    scale: config.scale,
    memoryMiB: config.memoryMiB,
  };
}

/**
 * Resolves a project's build and deploy settings from its config file and
 * package.json without building it.
 */
export async function resolveProject(
  opts: ProjectOptions = {},
): Promise<ResolvedProject> {
  const { cwd, config } = await loadProjectConfig(opts);
  return describeProject(cwd, config);
}

/**
 * Builds a project into a deployable zip file, the same way `gf build` does.
 * Logs go to the console unless the caller already configured LogTape.
 */
export async function build(opts: BuildOptions = {}): Promise<BuildResult> {
  if (getConfig() == null) {
    await configureLogger(opts.logLevel ?? "info");
  }

  const { cwd, config } = await loadProjectConfig(opts);
  const { files } = await buildFunctions(getLogger([pkg.name]), config);

  return { project: await describeProject(cwd, config), files };
}
