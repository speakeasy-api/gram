#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Undo a series of database migrations "
//MISE dir="{{ config_root }}/server"

import path from "node:path";
import fs from "node:fs/promises";
import { $, chalk } from "zx";

/**
 * Allows gum commands to use terminal colors and progress bars.
 */
const $g = $({ stdio: ["pipe", "pipe", "inherit"] });

async function main() {
  const migs = await fs.readFile("./migrations/atlas.sum", "utf-8");
  const [hash, ...lines] = migs.split("\n");
  if (!hash.startsWith("h1:")) {
    throw new Error(
      "Invalid atlas.sum file. Expected it to start with a line like h1:<base64-string>.",
    );
  }

  const choices: string[] = [];
  for (const line of lines.reverse()) {
    if (line.trim().length === 0) continue;

    const [file] = line.split(" ", 1);

    choices.push(file);
  }

  const { stdout } =
    await $g`gum choose ${choices} --height 10 --header "Select migrations target migration to rewind to"`;

  const target = stdout.trim();

  const drops: string[] = [];
  for (const c of choices) {
    if (c === target) break;
    drops.push(c);
  }

  if (drops.length === 0) {
    console.log("Nothing to do.");
    process.exit(0);
  }

  let droptxt = chalk.redBright(
    "The following migrations will be dropped:\n\n",
  );
  droptxt += chalk.redBright(drops.map((d) => `- ${d}`).join("\n"));
  droptxt += "\n\n";
  droptxt += chalk.yellowBright(
    "If any of these have been applied to production, you should stop and reconsider.\n",
  );
  droptxt += chalk.yellowBright("Are you sure you want to continue?");

  let confirmDrop = await $g`gum confirm ${droptxt}`.nothrow();
  if (confirmDrop.exitCode !== 0) {
    console.log("Aborting.");
    process.exit(0);
  }

  const targetId = target.split("_")[0];
  console.log(`Rewinding to migration ID ${chalk.cyanBright(targetId)}`);

  const proc =
    await $`atlas migrate down --to-version ${targetId} --url "$GRAM_DATABASE_URL" --dev-url "docker://pgvector/pgvector/pg17/dev?search_path=public"`.nothrow();
  if (proc.exitCode !== 0) {
    console.error(chalk.redBright("Failed to rewind migrations:"));
    console.error(proc.stderr);
    process.exit(1);
  }

  console.log("Done.\n");

  let pruneConfirm =
    await $g`gum confirm ${chalk.yellowBright("Delete dropped migrations and rehash?")}`.nothrow();
  if (pruneConfirm.exitCode !== 0) {
    console.log("Skipping prune.");
    process.exit(0);
  }

  for (const d of drops) {
    const p = path.join("./migrations", d);
    console.log(`Deleting ${chalk.yellowBright(p)}`);
    await fs.rm(p);
  }

  await $`mise run db:hash`;
}

await main();
