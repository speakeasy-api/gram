import { join } from "node:path";
import { buildFunctions } from "@gram-ai/functions/build";
import { $, chalk } from "zx";
import pkg from "../package.json" with { type: "json" };

async function build() {
  await buildFunctions({
    outDir: "dist",
    entrypoint: join(import.meta.dirname, "functions.ts"),
    export: "default",
  });

  const cmd = process.platform === "win32" ? ["where"] : ["command", "-v"];
  const program = "gram";
  const gramPath = await $`${cmd} ${program}`.nothrow();

  if (gramPath.exitCode !== 0) {
    throw new Error(
      `Gram CLI not found. Please install it from https://www.speakeasy.com/docs/gram/command-line/installation.`,
    );
  }

  const slug = pkg.name.split("/").pop();
  if (!slug) {
    throw new Error(
      `Could not determine function slug from package.json#name.`,
    );
  }

  console.log(chalk.greenBright(`Staging dist/gram.zip with slug: ${slug}`));
  await $`gram stage function --slug ${slug} --location dist/gram.zip`;

  console.log(chalk.greenBright(`Deploying with Gram CLI...`));
  await $({
    stdio: ["inherit", "inherit", "inherit"],
  })`gram push --config gram.json`;
}

if (import.meta.main) {
  await build().catch((err) => {
    console.error(err instanceof Error ? err.message : String(err));
    process.exit(1);
  });
}
