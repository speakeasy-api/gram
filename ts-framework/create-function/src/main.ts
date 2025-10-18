import { existsSync } from "node:fs";
import fs from "node:fs/promises";
import { join, resolve } from "node:path";
import process from "node:process";
import { $ } from "zx";
import { isCancel, log, taskLog } from "@clack/prompts";
import { parse } from "@bomb.sh/args";
import pkg from "../package.json" with { type: "json" };

import {
  confirmOrClack,
  selectOrClack,
  textOrClack,
  yn,
} from "./prompts/helpers.ts";

const packageNameRE = /^(@?[a-z0-9-_]+\/)?[a-z0-9-_]+$/;

const knownPackageManagers = new Set(["npm", "yarn", "pnpm", "bun", "deno"]);

function printUsage(packageManager: string): void {
  console.log(`
Usage:
  ${packageManager} create @gram-ai/function [options]

Options:
  --template <name>     Template to use (gram, mcp)
  --name <name>         Project name
  --dir <path>          Directory to create project in
  --git <yes|no>        Initialize git repository
  --install <yes|no>    Install dependencies
  -y, --yes             Skip all prompts and use defaults

Examples:
  ${packageManager} create @gram-ai/function
  ${packageManager} create @gram-ai/function --template mcp --name ecommerce
  ${packageManager} create @gram-ai/function --yes --template gram
`);
}

async function init(argv: string[]): Promise<void> {
  let packageManager = "npm";
  let detectedPM = process.env["npm_config_user_agent"]?.split("/")[0] || "";
  if (knownPackageManagers.has(detectedPM)) {
    packageManager = detectedPM;
  }

  const args = parse(argv, {
    alias: { y: "yes", h: "help" },
    string: ["template", "name", "dir", "git", "install"],
    boolean: ["yes", "help"],
  });

  if (args.help) {
    printUsage(packageManager);
    return;
  }

  const template = await selectOrClack<string>({
    message: "Pick a framework",
    options: [
      {
        value: "gram",
        label: "Gram",
        hint: "Simple framework focused on getting you up and runnning with minimal code.",
      },
      {
        value: "mcp",
        label: "MCP",
        hint: "Use the official @modelcontextprotocol/sdk package to build an MCP server and deploy it to Gram.",
      },
    ],
  })(args.template);
  if (isCancel(template)) {
    log.info("Operation cancelled.");
    return;
  }

  const nameArg = args.name?.trim();
  const name = await textOrClack({
    message: "What do you want to call your project?",
    defaultValue: "gram-mcp-server",
    validate: (value) => {
      if (packageNameRE.test(value || "")) {
        return undefined;
      }
      return [
        "Package names can be scoped or unscoped and must only contain alphanumeric characters, dashes and underscores.",
        "Examples:",
        "my-mcp-server",
        "@fancy-org/mcp-server",
      ].join(" ");
    },
  })(nameArg);
  if (isCancel(name)) {
    log.info("Operation cancelled.");
    return;
  }

  const rootDir = name.split("/").pop()?.trim() || "gram-func";
  const dirArg = args.dir?.trim();
  let dir = await textOrClack({
    message: "Where do you want to create it?",
    initialValue: rootDir,
    defaultValue: rootDir,
    validate: (value) => {
      const trimmed = value?.trim() || "";
      if (trimmed.length === 0) {
        return "Directory name cannot be empty.";
      }

      if (existsSync(trimmed)) {
        return `Directory ${trimmed} already exists. Please choose a different name.`;
      }

      return undefined;
    },
  })(dirArg);
  if (isCancel(dir)) {
    log.info("Operation cancelled.");
    return;
  }
  dir = dir.trim();

  const initGit = await confirmOrClack({
    message: "Initialize a git repository?",
  })(args.yes || yn(args.git ?? false));
  if (isCancel(initGit)) {
    log.info("Operation cancelled.");
    return;
  }

  const installDeps = await confirmOrClack({
    message: `Install dependencies with ${packageManager}?`,
  })(args.yes || yn(args.install ?? false));
  if (isCancel(installDeps)) {
    log.info("Operation cancelled.");
    return;
  }

  const tlog = taskLog({
    title: "Setting up project",
  });

  const isLocalDev = yn(process.env["GRAM_DEV"]);

  tlog.message("Scaffolding");
  const dirname = import.meta.dirname;
  const templateDir = resolve(join(dirname, "..", `gram-template-${template}`));
  await fs.cp(templateDir, dir, {
    recursive: true,
    filter: (src) => {
      let banned = src.includes(".git") || src.includes("NEXT_STEPS.txt");
      if (isLocalDev) {
        banned ||= src.includes("node_modules") || src.includes("dist");
      }
      return !banned;
    },
  });

  let gramFuncsVersion = pkg.devDependencies["@gram-ai/functions"];
  if (gramFuncsVersion == null || gramFuncsVersion.startsWith("workspace:")) {
    // This templating package and `@gram-ai/functions` are versioned in
    // lockstep so we can just use the matching version.
    gramFuncsVersion = `^${pkg.version}`;
  }
  if (isLocalDev && existsSync(resolve(dirname, "..", "..", "functions"))) {
    // For local development, use the local version of `@gram-ai/functions`
    // if it exists.
    const localPkgPath = resolve(dirname, "..", "..", "functions");
    gramFuncsVersion = `file:${localPkgPath}`;
    tlog.message(`Using local @gram-ai/functions from ${localPkgPath}`);
  }

  tlog.message("Updating package.json");
  const pkgPath = await fs.readFile(join(dir, "package.json"), "utf-8");
  const dstPkg = JSON.parse(pkgPath);
  dstPkg.name = name;
  const deps = dstPkg.dependencies;
  if (deps?.["@gram-ai/functions"] != null) {
    deps["@gram-ai/functions"] = gramFuncsVersion;
  }

  await fs.writeFile(
    join(dir, "package.json"),
    JSON.stringify(dstPkg, null, 2),
  );

  const contributingPath = join(dir, "CONTRIBUTING.md");
  if (existsSync(contributingPath)) {
    tlog.message("Creating symlinks for CONTRIBUTING.md");
    await fs.symlink("CONTRIBUTING.md", join(dir, "AGENTS.md"));
    await fs.symlink("CONTRIBUTING.md", join(dir, "CLAUDE.md"));
  }

  if (initGit) {
    tlog.message("Initializing git repository");
    await $`git init ${dir}`;
  }

  if (installDeps) {
    tlog.message(`Installing dependencies with ${packageManager}`);
    await $`cd ${dir} && ${packageManager} install`;
  }

  let successMessage = `All done! Run \`cd ${dir} && ${packageManager} run build\` to build your first Gram Function.`;
  successMessage = await fs
    .readFile(join(templateDir, "NEXT_STEPS.txt"), "utf-8")
    .catch(() => successMessage);
  successMessage = successMessage
    .replaceAll("$PACKAGE_MANAGER", packageManager)
    .replaceAll("$DIR", dir);

  tlog.success(successMessage);
}

try {
  await init(process.argv);
} catch (err) {
  log.error(`Unexpected error: ${err}`);
  process.exit(1);
}
