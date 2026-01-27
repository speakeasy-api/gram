import { getLogger, type Logger } from "@logtape/logtape";
import archiver from "archiver";
import esbuild from "esbuild";
import { existsSync } from "node:fs";
import { mkdir, open, readFile, stat, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import { createInterface } from "node:readline";
import { $, ProcessPromise, chalk } from "zx";
import { isCI, type ParsedUserConfig } from "./config.ts";

type Artifacts = {
  funcFilename: string;
  manifestFilename: string;
  zipFilename: string;
};

async function resolveArtifacts(cfg: ParsedUserConfig): Promise<Artifacts> {
  return {
    funcFilename: join(cfg.outDir, "functions.js"),
    manifestFilename: join(cfg.outDir, "manifest.json"),
    zipFilename: join(cfg.outDir, "gram.zip"),
  };
}

function resolveGramCLI(): string {
  // Check for local development mode using GRAM_DEV env var
  const isLocalDev =
    process.env["GRAM_DEV"]?.toLowerCase() === "true" ||
    process.env["GRAM_DEV"] === "1";

  if (isLocalDev) {
    // In local dev, use the CLI from cli/bin/gram relative to workspace root
    // From ts-framework/functions/src/build -> ../../../../cli/bin/gram
    const localCliPath = resolve(
      dirname(new URL(import.meta.url).pathname),
      "../../../../cli/bin/gram",
    );

    // Check if the local CLI exists
    if (existsSync(localCliPath)) {
      return localCliPath;
    }
  }

  // Use system-installed gram
  return "gram";
}

export async function buildFunctions(logger: Logger, cfg: ParsedUserConfig) {
  const cwd = cfg.cwd ?? process.cwd();
  const entrypoint = join(cwd, cfg.entrypoint);
  const exp = await import(resolve(entrypoint)).then((mod) => {
    return mod.default; // If this is a Promise (then-able) then it will be resolved
  });

  const manifestFunc =
    "manifest" in exp && typeof exp.manifest === "function"
      ? exp.manifest.bind(exp)
      : null;
  if (!manifestFunc) {
    throw new Error(
      `The function entrypoint ${cfg.entrypoint} does not export an object with a manifest() function.`,
    );
  }

  logger.info("Building Gram Function");

  const manifest = await manifestFunc();

  const artifacts = await resolveArtifacts(cfg);

  await mkdir(cfg.outDir, { recursive: true });
  await writeFile(
    artifacts.manifestFilename,
    JSON.stringify(manifest, null, 2),
  );

  await bundleFunction(logger, {
    entrypoint,
    outFile: artifacts.funcFilename,
    requireInterop: cfg.requireInterop,
  });

  await createZipArchive(logger, artifacts);

  const zipstats = await stat(artifacts.zipFilename);

  logger.info(
    `Built Gram Function ZIP: ${artifacts.zipFilename} (${(zipstats.size / 1024).toFixed(2)} KiB)`,
  );

  return {
    files: [{ path: artifacts.zipFilename, size: zipstats.size }],
  };
}

async function inferSlug(cwd: string): Promise<string> {
  const result = await resolvePackageJson(cwd);
  if (!result) {
    throw new Error(`Could not find package.json in ${cwd} or any parent dir.`);
  }
  const [pkgFilename, pkg] = result;
  const name = pkg?.name;
  const slug = name?.split("/").pop();
  if (!slug) {
    throw new Error(
      `Could not determine function slug from ${pkgFilename}#name.`,
    );
  }

  return slug;
}

async function bundleFunction(
  logger: Logger,
  options: {
    entrypoint: string;
    outFile: string;
    requireInterop: boolean;
  },
): Promise<Array<{ path: string; hash: string }>> {
  logger.info(
    `Bundling function from ${options.entrypoint} into ${options.outFile}`,
  );

  let banner: Record<string, string> | undefined = undefined;
  if (options.requireInterop) {
    banner = {
      js: [
        `import { createRequire as topLevelCreateRequire } from 'node:module';`,
        `if (typeof require === 'undefined') { globalThis.require = topLevelCreateRequire(import.meta.url); }`,
      ].join("\n"),
    };
  }

  const res = await esbuild.build({
    entryPoints: [options.entrypoint],
    outfile: options.outFile,
    bundle: true,
    treeShaking: true,
    minify: true,
    platform: "node",
    target: ["node22"],
    format: "esm",
    banner,
  });

  return (
    res.outputFiles?.map((f) => ({
      path: f.path,
      hash: f.hash,
    })) || []
  );
}

async function createZipArchive(
  logger: Logger,
  artifacts: Artifacts,
): Promise<void> {
  const { zipFilename, funcFilename, manifestFilename } = artifacts;
  logger.info(`Creating ZIP archive of function in ${zipFilename}`);

  const archive = archiver("zip", { zlib: { level: 9 } });
  const { promise, resolve: res, reject: rej } = Promise.withResolvers<void>();
  archive.on("error", rej);
  archive.on("close", res);

  const output = await open(zipFilename, "w");
  archive.pipe(output.createWriteStream());
  archive.file(manifestFilename, { name: "manifest.json" });
  archive.file(funcFilename, { name: "functions.js" });
  await archive.finalize();
  await promise;
  await output.close();
}

export async function deployFunction(logger: Logger, config: ParsedUserConfig) {
  const cwd = config.cwd ?? process.cwd();
  const slug = config.slug || (await inferSlug(cwd));

  const gramCLI = resolveGramCLI();
  // Only check if CLI exists when using system gram
  if (gramCLI === "gram") {
    const cmd = process.platform === "win32" ? ["where"] : ["command", "-v"];
    const gramPath = await $`${cmd} ${gramCLI}`.nothrow();

    if (gramPath.exitCode !== 0) {
      throw new Error(
        `Gram CLI not found. Please install it from https://www.speakeasy.com/docs/gram/command-line/installation.`,
      );
    }
  }

  const artifacts = await resolveArtifacts(config);
  const { zipFilename } = artifacts;

  const stageArgs = [
    "--config",
    config.deployStagingFile,
    "function",
    "--slug",
    slug,
    "--location",
    relative(dirname(config.deployStagingFile), zipFilename),
  ];
  logger.info(`Staging ${zipFilename} with slug: ${slug}`);
  await $`${gramCLI} stage ${stageArgs}`;

  const pushArgs = [
    "--log-pretty=false",
    "--api-url",
    "http://localhost:8080",
    "push",
    "--config",
    config.deployStagingFile,
  ];
  if (config.deployProject) {
    pushArgs.push("--project", config.deployProject);
  }

  logger.info("Deploying function with Gram CLI");

  const pushcmd = $({
    stdio: ["pipe", "pipe", "pipe"],
  })`${gramCLI} ${pushArgs}`
    .quiet()
    .nothrow();

  // Consume stdio and show loader concurrently
  const stdioTask = consumeStdio(pushcmd, getLogger(["gram", "cli"]));

  const result = await Promise.all([stdioTask, pushcmd]).then(
    ([, result]) => result,
  );

  if (result.exitCode !== 0) {
    throw new Error(
      `Gram CLI push command failed with exit code ${result.exitCode}`,
    );
  }

  logger.info("Gram Function deployed successfully");

  await handleOpenBrowser(logger, cwd, config);
}

async function resolvePackageJson(
  cwd: string,
): Promise<[filename: string, value: { name?: string | undefined }] | null> {
  let currentDir = resolve(cwd);
  const root = resolve("/");

  while (currentDir !== root) {
    const packageJsonPath = join(currentDir, "package.json");
    try {
      const fstat = await stat(packageJsonPath);
      if (!fstat.isFile()) {
        throw new Error("Not a file");
      }
      const value = await tryLoadPackageJson(packageJsonPath);
      return [packageJsonPath, value];
    } catch (e) {
      currentDir = resolve(currentDir, "..");
    }
  }

  return null;
}

async function tryLoadPackageJson(
  filename: string,
): Promise<{ name?: string | undefined }> {
  const pkgModule = await readFile(filename, "utf-8");
  const parsed = JSON.parse(pkgModule);
  if (typeof parsed !== "object" || parsed == null) {
    throw new Error("package.json is not an object");
  }

  const rawName = parsed.name;
  let name: string | undefined;
  if (typeof rawName === "string" && rawName.length > 0) {
    name = rawName;
  }

  return { name };
}

async function consumeStdio(proc: ProcessPromise, logger: Logger) {
  const stdoutReader = createInterface({
    input: proc.stdout,
    crlfDelay: Infinity,
  });

  const stderrReader = createInterface({
    input: proc.stderr,
    crlfDelay: Infinity,
  });

  // Process both streams in parallel
  await Promise.all([
    (async () => {
      for await (const line of stdoutReader) {
        logCLIOutput(logger, line);
      }
    })(),
    (async () => {
      for await (const line of stderrReader) {
        logCLIOutput(logger, line);
      }
    })(),
  ]);
}

function logCLIOutput(logger: Logger, line: string) {
  if (line.trim() === "") {
    return;
  }

  let obj: unknown;
  try {
    obj = JSON.parse(line);
  } catch (e) {
    // We'll assume the line isn't a well-formed JSON log event. It will be
    // logged as-is below.
    obj = null;
  }

  if (obj == null) {
    logger.info(line);
    return;
  }

  if (typeof obj !== "object") {
    logger.info(line);
    return;
  }

  const { msg, level, ...rest } = obj as Record<string, unknown>;
  delete rest["source"];
  delete rest["time"];

  if (typeof msg !== "string") {
    logger.info(line);
    return;
  }

  const lvl = typeof level === "string" ? level.toLowerCase() : "";

  if (lvl === "debug") {
    logger.debug(msg, rest);
  } else if (lvl === "info") {
    logger.info(msg, rest);
  } else if (lvl === "warn" || lvl === "warning") {
    logger.warn(msg, rest);
  } else if (lvl === "error") {
    logger.error(msg, rest);
  } else {
    logger.info(msg, rest);
  }
}

export async function handleOpenBrowser(
  logger: Logger,
  cwd: string,
  cfg: ParsedUserConfig,
) {
  if (cfg.openBrowserAfterDeploy === false || isCI) {
    return;
  }

  // Always prompt unless explicitly disabled
  const shouldOpen = await promptYesNo(
    "Would you like to open the Gram dashboard to create an MCP server?",
  );

  if (shouldOpen) {
    await openBrowser(logger, "https://app.getgram.ai?from=cli");
  } else {
    // Only persist when user says no
    await updateConfigFile(cwd, false);
  }
}

async function promptYesNo(question: string): Promise<boolean> {
  const rl = createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  const icon = chalk.cyan("●");
  const q = chalk.bold(question);
  const choices = chalk.grey("(y/n)");

  process.stdout.write(`${icon} ${q}\n`);

  return new Promise((resolve) => {
    rl.question(`${choices} `, (answer) => {
      rl.close();
      const normalized = answer.trim().toLowerCase();
      resolve(normalized === "y" || normalized === "yes");
    });
  });
}

async function openBrowser(logger: Logger, url: string) {
  try {
    if (process.platform === "darwin") {
      await $`open ${url}`;
    } else if (process.platform === "win32") {
      await $`start ${url}`;
    } else {
      await $`xdg-open ${url}`;
    }
    logger.info(`Opened ${url} in browser`);
  } catch (e) {
    logger.warn("Failed to open browser", {
      error: e instanceof Error ? e.message : String(e),
    });
  }
}

async function updateConfigFile(cwd: string, shouldOpen: boolean) {
  const configFiles = [
    "gram.config.ts",
    "gram.config.mts",
    "gram.config.js",
    "gram.config.mjs",
  ];

  for (const configFile of configFiles) {
    const configPath = join(cwd, configFile);
    try {
      await stat(configPath);
      const content = await readFile(configPath, "utf-8");

      // Add openBrowserAfterDeploy to the config
      const updatedContent = addOpenBrowserConfig(content, shouldOpen);
      await writeFile(configPath, updatedContent, "utf-8");
      return;
    } catch (e) {
      // File doesn't exist, try next
      continue;
    }
  }

  // No config file exists, create one
  const newConfigPath = join(cwd, "gram.config.ts");
  const newConfig = `import { defineConfig } from "@gram-ai/functions/build";

export default defineConfig({
  openBrowserAfterDeploy: ${shouldOpen},
});
`;
  await writeFile(newConfigPath, newConfig, "utf-8");
}

function addOpenBrowserConfig(content: string, shouldOpen: boolean): string {
  // Check if openBrowserAfterDeploy already exists
  if (content.includes("openBrowserAfterDeploy")) {
    return content;
  }

  // Try to add it to existing config object
  const configObjectMatch = content.match(/defineConfig\s*\(\s*\{([^}]*)\}/s);
  if (configObjectMatch) {
    const [fullMatch, innerContent] = configObjectMatch;
    const hasTrailingComma = innerContent?.trim().endsWith(",");
    const insertion = `${hasTrailingComma ? "" : ","}\n  openBrowserAfterDeploy: ${shouldOpen},`;
    return content.replace(
      fullMatch,
      fullMatch.replace(/\}$/, `${insertion}\n}`),
    );
  }

  // Fallback: just append at the end
  return content;
}
