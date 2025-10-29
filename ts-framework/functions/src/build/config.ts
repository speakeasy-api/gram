import { stat } from "node:fs/promises";
import path from "node:path";
import * as z from "zod";

const userConfigSchema = z.object({
  /**
   * The path to the entrypoint file for the application. This must export
   * functions that confirm to the Gram Functions interface or a single value
   * that provides these.
   */
  entrypoint: z.string().default(path.join("src", "gram.ts")),
  /**
   * The output directory where build artifacts should be written.
   */
  outDir: z.string().default("dist"),
  /**
   * The current working directory to use when resolving paths.
   */
  cwd: z.string().default("."),
  /**
   * Deploy a Gram Function with the Gram CLI after building it.
   */
  deploy: z.boolean().default(true),
  /**
   * The Gram project to deploy to. If this is not set, then the Gram CLI will
   * use the project that was chosen when `gram auth` was run.
   */
  deployProject: z.string().optional(),
  /**
   * The deployment configuration file to stage the function to and submit to
   * the Gram CLI.
   */
  deployStagingFile: z.string().default("gram.deploy.json"),
  /**
   * The slug to use for the function when deploying to Gram. If this option is
   * not set then the slug will be inferred from the nearest `package.json` file
   * using the `name` field.
   */
  slug: z.string().optional(),
  /**
   * Whether to open the browser after deploying. If not set, the user will be
   * prompted once and their choice will be remembered.
   */
  openBrowserAfterDeploy: z.boolean().optional(),
});

export type UserConfig = z.input<typeof userConfigSchema>;
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
