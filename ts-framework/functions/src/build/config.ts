import { stat } from "node:fs/promises";
import path from "node:path";
import * as z from "zod";

export const isCI = z.stringbool().catch(false).parse(process.env["CI"]);

export type UserConfig = {
  /**
   * The path to the entrypoint file for the application. This must export
   * functions that confirm to the Gram Functions interface or a single value
   * that provides these.
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
   */
  deployStagingFile?: string | undefined;
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
};

const userConfigSchema = z.object({
  entrypoint: z.string().default(path.join("src", "gram.ts")),
  outDir: z.string().default("dist"),
  cwd: z.string().default("."),
  deployProject: z.string().optional(),
  deployStagingFile: z.string().default("gram.deploy.json"),
  slug: z.string().optional(),
  openBrowserAfterDeploy: z.boolean().optional(),
}) satisfies z.ZodType<UserConfig>;

export type ParsedUserConfig = z.output<typeof userConfigSchema>;

export function defineConfig(config: UserConfig): UserConfig {
  return config;
}

export async function loadConfig(
  configPath?: string | undefined,
): Promise<z.ZodSafeParseResult<ParsedUserConfig>> {
  if (!configPath) {
    return userConfigSchema.safeParse({});
  }

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

  return userConfigSchema.safeParse(mod.default);
}
