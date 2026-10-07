import { existsSync } from "node:fs";
import { stat } from "node:fs/promises";
import path from "node:path";
import * as z from "zod";

export const isCI = z.stringbool().catch(false).parse(process.env["CI"]);

/**
 * Config file names looked up in the project directory, in order. The
 * `gram.config.*` names are deprecated and still read.
 */
export const CONFIG_FILE_NAMES = [
  "speakeasy.config.ts",
  "speakeasy.config.mts",
  "speakeasy.config.js",
  "speakeasy.config.mjs",
  "gram.config.ts",
  "gram.config.mts",
  "gram.config.js",
  "gram.config.mjs",
];

const LEGACY_CONFIG_PREFIX = "gram.config.";

/** The entrypoint used when the config does not set one. */
const DEFAULT_ENTRYPOINT = path.join("src", "functions.ts");

/** The entrypoint used when {@link DEFAULT_ENTRYPOINT} does not exist. */
const LEGACY_ENTRYPOINT = path.join("src", "gram.ts");

/** The deployment file used when the config does not set one. */
const DEFAULT_DEPLOY_STAGING_FILE = "speakeasy.deploy.json";

/**
 * The deprecated deployment file name, used when it exists and
 * {@link DEFAULT_DEPLOY_STAGING_FILE} does not.
 */
const LEGACY_DEPLOY_STAGING_FILE = "gram.deploy.json";

/** Prints a one-line note on stderr that a file name is deprecated. */
function noteRename(legacy: string, current: string): void {
  process.stderr.write(
    `Note: ${legacy} is deprecated and still works. Rename it to ${current}.\n`,
  );
}

/**
 * Returns the first config file that exists in dir, if any, and notes when it
 * has a deprecated `gram.config.*` name.
 */
export function findConfigFile(dir: string): string | undefined {
  const file = CONFIG_FILE_NAMES.map((name) => path.join(dir, name)).find(
    (candidate) => existsSync(candidate),
  );
  const name = file && path.basename(file);
  if (name?.startsWith(LEGACY_CONFIG_PREFIX)) {
    noteRename(name, name.replace(LEGACY_CONFIG_PREFIX, "speakeasy.config."));
  }
  return file;
}

/**
 * Returns preferred, or legacy when only legacy exists in dir. Both are
 * relative to dir.
 */
function preferExisting(
  dir: string,
  preferred: string,
  legacy: string,
): { file: string; legacy: boolean } {
  if (
    !existsSync(path.join(dir, preferred)) &&
    existsSync(path.join(dir, legacy))
  ) {
    return { file: legacy, legacy: true };
  }
  return { file: preferred, legacy: false };
}

export type UserConfig = {
  /**
   * The path to the entrypoint file for the application. This must export
   * functions that confirm to the Gram Functions interface or a single value
   * that provides these.
   *
   * @default "src/functions.ts", or "src/gram.ts" when only that file exists
   */
  entrypoint?: string | undefined;
  /**
   * The output directory where build artifacts should be written.
   */
  outDir?: string | undefined;
  /**
   * The current working directory to use when resolving paths.
   */
  cwd?: string | undefined;
  /**
   * The Gram project to deploy to. If this is not set, then the Gram CLI will
   * use the project that was chosen when `gram auth` was run.
   */
  deployProject?: string | undefined;
  /**
   * The deployment configuration file to stage the function to and submit to
   * the Gram CLI.
   *
   * @default "speakeasy.deploy.json", or "gram.deploy.json" when only that
   * file exists
   */
  deployStagingFile?: string | undefined;
  /**
   * The number of instances to run for the function when deployed to Gram.
   */
  scale?: number | undefined;
  /**
   * The memory limit in MiB of function runner machines when deployed to Gram.
   */
  memoryMiB?: number | undefined;

  /**
   * The slug to use for the function when deploying to Gram. If this option is
   * not set then the slug will be inferred from the nearest `package.json` file
   * using the `name` field.
   */
  slug?: string | undefined;
  /**
   * Whether to open the browser after deploying. If not set, the user will be
   * prompted once and their choice will be remembered.
   */
  openBrowserAfterDeploy?: boolean | undefined;

  /**
   * Emit code to enable the use of dynamic `require()` calls in bundled code.
   *
   * @default true
   */
  requireInterop?: boolean | undefined;
};

const userConfigSchema = z.object({
  entrypoint: z.string().optional(),
  outDir: z.string().default("dist"),
  cwd: z.string().default("."),
  deployProject: z.string().optional(),
  deployStagingFile: z.string().optional(),
  slug: z.string().optional(),
  scale: z.number().int().positive().optional(),
  memoryMiB: z.number().int().positive().optional(),
  openBrowserAfterDeploy: z.boolean().optional(),
  requireInterop: z.boolean().default(true),
}) satisfies z.ZodType<UserConfig>;

export type ParsedUserConfig = z.output<typeof userConfigSchema> & {
  entrypoint: string;
  deployStagingFile: string;
};

export function defineConfig(config: UserConfig): UserConfig {
  return config;
}

/**
 * Loads a config file, or the defaults when configPath is unset. Relative
 * paths in the config resolve against baseDir, which is where the default
 * entrypoint and deployment file are looked up.
 */
export async function loadConfig(
  configPath?: string | undefined,
  baseDir: string = process.cwd(),
): Promise<ParsedUserConfig> {
  let raw: unknown = {};
  if (configPath) {
    configPath = path.resolve(configPath);

    const fstat = await stat(configPath);
    if (!fstat.isFile()) {
      throw new Error(`Config path is not a file: ${configPath}`);
    }

    const mod = await import(configPath);
    if (!mod.default || typeof mod.default !== "object") {
      throw new Error(
        `Config file does not export a default config value: ${configPath}`,
      );
    }
    raw = mod.default;
  }

  const config = userConfigSchema.parse(raw);
  return {
    ...config,
    entrypoint:
      config.entrypoint ??
      preferExisting(
        path.resolve(baseDir, config.cwd),
        DEFAULT_ENTRYPOINT,
        LEGACY_ENTRYPOINT,
      ).file,
    deployStagingFile:
      config.deployStagingFile ?? defaultDeployStagingFile(baseDir),
  };
}

function defaultDeployStagingFile(dir: string): string {
  const { file, legacy } = preferExisting(
    dir,
    DEFAULT_DEPLOY_STAGING_FILE,
    LEGACY_DEPLOY_STAGING_FILE,
  );
  if (legacy) {
    noteRename(LEGACY_DEPLOY_STAGING_FILE, DEFAULT_DEPLOY_STAGING_FILE);
  }
  return file;
}
