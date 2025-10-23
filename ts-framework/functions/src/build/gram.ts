import { mkdir, open, readFile, stat, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import esbuild from "esbuild";
import archiver from "archiver";
import { $, ProcessPromise } from "zx";
import { getLogger, type Logger } from "@logtape/logtape";

import { Gram } from "../framework.ts";
import type { ParsedUserConfig } from "./config.ts";
import { createInterface } from "node:readline";

export async function buildFunctions(logger: Logger, cfg: ParsedUserConfig) {
  const cwd = cfg.cwd ?? process.cwd();
  const entrypoint = resolve(cwd, cfg.entrypoint);
  const gram = await import(entrypoint).then((mod) => {
    const exportsym = cfg.export;
    const gramExport = mod[exportsym];
    if (!(gramExport instanceof Gram)) {
      throw new Error(
        `Export "${exportsym}" does not appear to be an instance of Gram`,
      );
    }

    return gramExport;
  });

  const slug = cfg.slug || (await inferSlug(cwd));

  logger.info(`Building Gram Function: ${slug}`);

  const manifest = gram.manifest();

  await mkdir(cfg.outDir, { recursive: true });
  const outFile = resolve(cfg.outDir, "manifest.json");
  await writeFile(outFile, JSON.stringify(manifest, null, 2));

  await bundleFunction(logger, {
    entrypoint,
    outFile: resolve(cfg.outDir, "functions.js"),
  });

  const zipPath = await createZipArchive(logger, { outDir: cfg.outDir });

  if (cfg.deploy) {
    await deployFunction(logger, {
      project: cfg.deployProject,
      stagingFile: cfg.deployStagingFile,
      slug,
      zipPath,
    });
  }

  const zipstats = await stat(zipPath);
  return {
    files: [{ path: zipPath, size: zipstats.size }],
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
  },
): Promise<Array<{ path: string; hash: string }>> {
  logger.info(
    `Bundling function from ${options.entrypoint} into ${options.outFile}`,
  );

  const res = await esbuild.build({
    entryPoints: [options.entrypoint],
    outfile: options.outFile,
    bundle: true,
    treeShaking: true,
    minify: true,
    platform: "node",
    target: ["node22"],
    format: "esm",
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
  cfg: { outDir: string },
): Promise<string> {
  const zipPath = resolve(cfg.outDir, "gram.zip");
  logger.info(`Creating ZIP archive of function in ${zipPath}`);

  const archive = archiver("zip", { zlib: { level: 9 } });
  const { promise, resolve: res, reject: rej } = Promise.withResolvers<void>();
  archive.on("error", rej);
  archive.on("close", res);

  const output = await open(zipPath, "w");
  archive.pipe(output.createWriteStream());
  archive.file(join(cfg.outDir, "manifest.json"), {
    name: "manifest.json",
  });
  archive.file(join(cfg.outDir, "functions.js"), { name: "functions.js" });
  await archive.finalize();
  await promise;
  await output.close();

  return zipPath;
}

async function deployFunction(
  logger: Logger,
  options: {
    project?: string | undefined;
    slug: string;
    stagingFile: string;
    zipPath: string;
  },
) {
  const cmd = process.platform === "win32" ? ["where"] : ["command", "-v"];
  const program = "gram";
  const gramPath = await $`${cmd} ${program}`.nothrow();

  if (gramPath.exitCode !== 0) {
    throw new Error(
      `Gram CLI not found. Please install it from https://www.speakeasy.com/docs/gram/command-line/installation.`,
    );
  }

  const { slug, zipPath } = options;

  const stageArgs = [
    "--config",
    options.stagingFile,
    "function",
    "--slug",
    slug,
    "--location",
    relative(dirname(options.stagingFile), zipPath),
  ];
  logger.info(`Staging ${zipPath} with slug: ${slug}`);
  await $`gram stage ${stageArgs}`;

  const pushArgs = [
    "--log-pretty=false",
    "--api-url",
    "http://localhost:8080",
    "push",
    "--config",
    options.stagingFile,
  ];
  if (options.project) {
    pushArgs.push("--project", options.project);
  }

  logger.info("Deploying function with Gram CLI");
  const pushcmd = $({
    stdio: ["pipe", "pipe", "pipe"],
  })`gram ${pushArgs}`
    .quiet()
    .nothrow();

  await consumeStdio(pushcmd, getLogger(["gram", "cli"]));

  const result = await pushcmd;

  if (result.exitCode !== 0) {
    throw new Error(
      `Gram CLI push command failed with exit code ${result.exitCode}`,
    );
  }

  logger.info("Gram Function deployed successfully");
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
    logger.error(line);
    return;
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
