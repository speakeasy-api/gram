#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Update and validate user-facing agent skills in skills/"
//MISE dir="{{ config_root }}"

//USAGE flag "--bump <level>" help="Bump plugin version: patch, minor, or major"
//USAGE flag "--fix" help="Auto-fix issues where possible (e.g. update CLI flags in skills)"
//USAGE flag "--quiet" help="Only output errors and warnings"

import { readdir, readFile, writeFile, stat } from "node:fs/promises";
import { join, basename } from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { chalk } from "zx";

const exec = promisify(execFile);

const SKILLS_DIR = "skills/skills";
const PLUGIN_JSON = "skills/.claude-plugin/plugin.json";
const bumpLevel = process.env["usage_bump"] || "";
const autoFix = process.env["usage_fix"] === "true";
const quiet = process.env["usage_quiet"] === "true";

interface CliCommand {
  name: string;
  flags: string[];
  subcommands: string[];
}

interface ValidationResult {
  errors: string[];
  warnings: string[];
  fixes: string[];
}

const result: ValidationResult = { errors: [], warnings: [], fixes: [] };

function info(msg: string) {
  if (!quiet) console.log(msg);
}

function error(msg: string) {
  result.errors.push(msg);
  console.error(chalk.red(`  ✘ ${msg}`));
}

function warn(msg: string) {
  result.warnings.push(msg);
  console.warn(chalk.yellow(`  ⚠ ${msg}`));
}

function fix(msg: string) {
  result.fixes.push(msg);
  console.log(chalk.green(`  ✔ ${msg}`));
}

// ─────────────────────────────────────────────
// Step 1: Sync CLI help — capture current flags
// ─────────────────────────────────────────────

async function captureCliHelp(): Promise<Map<string, CliCommand>> {
  const commands = new Map<string, CliCommand>();

  const topLevel = [
    "gram",
    "gram auth",
    "gram stage",
    "gram stage openapi",
    "gram stage function",
    "gram push",
    "gram upload",
    "gram status",
    "gram whoami",
    "gram install",
    "gram install claude-code",
    "gram install claude-desktop",
    "gram install cursor",
    "gram install gemini-cli",
  ];

  for (const cmd of topLevel) {
    const args = cmd.split(" ").slice(1); // drop "gram"
    args.push("--help");

    try {
      const { stdout } = await exec("gram", args, { timeout: 10_000 });
      const flags: string[] = [];
      const subcommands: string[] = [];

      let inOptions = false;
      let inCommands = false;

      for (const line of stdout.split("\n")) {
        const trimmed = line.trim();

        if (/^(OPTIONS|GLOBAL OPTIONS):?\s*$/i.test(trimmed)) {
          inOptions = true;
          inCommands = false;
          continue;
        }
        if (/^COMMANDS:?\s*$/i.test(trimmed)) {
          inCommands = true;
          inOptions = false;
          continue;
        }
        if (
          trimmed === "" ||
          /^(USAGE|DESCRIPTION|NAME):?\s*$/i.test(trimmed)
        ) {
          inOptions = false;
          inCommands = false;
          continue;
        }

        if (inOptions) {
          // Extract flag names like --foo, -f
          const flagMatch = trimmed.match(/^(--?\S+(?:,\s+--?\S+)*)/);
          if (flagMatch) {
            const raw = flagMatch[1];
            for (const f of raw.split(/,\s*/)) {
              const name = f.replace(/\s+.*$/, "").trim();
              if (name.startsWith("-")) flags.push(name);
            }
          }
        }

        if (inCommands) {
          const cmdMatch = trimmed.match(/^(\S+)/);
          if (cmdMatch && cmdMatch[1] !== "help," && cmdMatch[1] !== "help") {
            subcommands.push(cmdMatch[1]);
          }
        }
      }

      commands.set(cmd, { name: cmd, flags, subcommands });
    } catch (e: any) {
      warn(`Failed to capture help for '${cmd}': ${e.message}`);
    }
  }

  return commands;
}

// Flags that are global / covered by gram-context — don't warn about these in other skills
const GLOBAL_FLAGS = new Set([
  "--help",
  "-h",
  "--version",
  "-v",
  "--log-level",
  "--log-pretty",
  "--api-key",
  "--project",
  "--org",
  "--profile",
]);

// Extract all --flag mentions anywhere in the skill (for "undocumented" checks)
function extractAllMentionedFlags(content: string): Set<string> {
  const flags = new Set<string>();
  for (const match of content.matchAll(/--([\w][\w-]*)/g)) {
    flags.add(`--${match[1]}`);
  }
  return flags;
}

// Extract flags from markdown tables only (for "removed flag" checks)
function extractTableDocumentedFlags(content: string): Set<string> {
  const flags = new Set<string>();
  for (const line of content.split("\n")) {
    if (!line.trim().startsWith("|")) continue;
    if (/^\|\s*-+/.test(line.trim())) continue;

    for (const match of line.matchAll(/`(--\w[\w-]*)`/g)) {
      flags.add(match[1]);
    }
  }
  return flags;
}

// Map skill names to the CLI commands they document
const SKILL_COMMAND_MAP: Record<string, string[]> = {
  "gram-context": [
    "gram",
    "gram auth",
    "gram stage openapi",
    "gram stage function",
    "gram push",
    "gram upload",
    "gram status",
    "gram whoami",
    "gram install",
  ],
  "deploy-openapi": ["gram stage openapi", "gram push", "gram upload"],
  "deploy-functions": ["gram stage function", "gram push", "gram upload"],
  "install-mcp-server": [
    "gram install",
    "gram install claude-code",
    "gram install claude-desktop",
    "gram install cursor",
    "gram install gemini-cli",
  ],
  "check-deployment-status": ["gram status", "gram whoami"],
  "write-gram-function": [],
};

async function syncCliHelp(commands: Map<string, CliCommand>) {
  info(chalk.bold("\n● Syncing CLI help into skills\n"));

  const skillDirs = await readdir(SKILLS_DIR);

  for (const skillName of skillDirs) {
    const skillPath = join(SKILLS_DIR, skillName, "SKILL.md");
    const relevantCommands = SKILL_COMMAND_MAP[skillName];
    if (!relevantCommands || relevantCommands.length === 0) continue;

    let content: string;
    try {
      content = await readFile(skillPath, "utf-8");
    } catch {
      continue;
    }

    // All --flag mentions (code blocks, prose, tables) for "undocumented" checks
    const allMentioned = extractAllMentionedFlags(content);
    // Only table-documented flags for "removed flag" checks
    const tableFlags = extractTableDocumentedFlags(content);

    // Collect all CLI flags across relevant commands
    const allCliFlags = new Set<string>();
    for (const cmdName of relevantCommands) {
      const cmd = commands.get(cmdName);
      if (cmd) cmd.flags.forEach((f) => allCliFlags.add(f));
    }

    // Check for CLI flags not mentioned anywhere in the skill
    for (const flag of allCliFlags) {
      if (allMentioned.has(flag)) continue;
      if (GLOBAL_FLAGS.has(flag) && skillName !== "gram-context") continue;
      warn(`${skillName}: CLI flag '${flag}' not mentioned anywhere in skill`);
    }

    // Check for table-documented flags that no longer exist in CLI
    for (const docFlag of tableFlags) {
      if (GLOBAL_FLAGS.has(docFlag)) continue;
      if (allCliFlags.has(docFlag)) continue;
      warn(`${skillName}: table-documented flag '${docFlag}' not found in CLI`);
    }
  }
}

// ─────────────────────────────────────────────
// Step 2: Validate structure
// ─────────────────────────────────────────────

function parseFrontmatter(content: string): Record<string, string> | null {
  const match = content.match(/^---\n([\s\S]*?)\n---/);
  if (!match) return null;

  const fields: Record<string, string> = {};
  // Simple YAML parser for flat key-value (handles multiline >- descriptions)
  let currentKey = "";
  let currentValue = "";

  for (const line of match[1].split("\n")) {
    const kvMatch = line.match(/^(\w[\w-]*):\s*(.*)/);
    if (kvMatch) {
      if (currentKey) fields[currentKey] = currentValue.trim();
      currentKey = kvMatch[1];
      const val = kvMatch[2];
      if (val === ">-" || val === "|") {
        currentValue = "";
      } else {
        currentValue = val;
      }
    } else if (currentKey) {
      currentValue += " " + line.trim();
    }
  }
  if (currentKey) fields[currentKey] = currentValue.trim();

  return fields;
}

async function validateStructure() {
  info(chalk.bold("\n● Validating skill structure\n"));

  let skillDirs: string[];
  try {
    skillDirs = await readdir(SKILLS_DIR);
  } catch {
    error(`Skills directory '${SKILLS_DIR}' not found`);
    return;
  }

  // Filter to actual directories
  const skills: string[] = [];
  for (const name of skillDirs) {
    const s = await stat(join(SKILLS_DIR, name));
    if (s.isDirectory()) skills.push(name);
  }

  info(`  Found ${skills.length} skills: ${skills.join(", ")}`);

  for (const skillName of skills) {
    const skillPath = join(SKILLS_DIR, skillName, "SKILL.md");
    let content: string;
    try {
      content = await readFile(skillPath, "utf-8");
    } catch {
      error(`${skillName}: missing SKILL.md`);
      continue;
    }

    // Check YAML frontmatter
    const fm = parseFrontmatter(content);
    if (!fm) {
      error(`${skillName}: missing or invalid YAML frontmatter`);
      continue;
    }
    if (!fm.name) error(`${skillName}: frontmatter missing 'name'`);
    if (!fm.description)
      error(`${skillName}: frontmatter missing 'description'`);
    if (!fm.license) warn(`${skillName}: frontmatter missing 'license'`);

    // Check trigger phrases in description
    if (fm.description) {
      const hasTriggers =
        fm.description.includes('"') &&
        fm.description.toLowerCase().includes("trigger");
      if (!hasTriggers) {
        warn(`${skillName}: description may be missing trigger phrases`);
      }
    }

    // Check name matches directory
    if (fm.name && fm.name !== skillName) {
      warn(
        `${skillName}: frontmatter name '${fm.name}' doesn't match directory name`,
      );
    }

    // Check line count
    const lines = content.split("\n").length;
    if (lines > 500) {
      warn(`${skillName}: ${lines} lines (over 500 line limit)`);
    } else {
      info(`  ${skillName}: ${lines} lines ✓`);
    }

    // Check cross-references
    const relatedSection = content.match(
      /## Related Skills\n([\s\S]*?)(?=\n##|\n$|$)/,
    );
    if (relatedSection) {
      const refs = [...relatedSection[1].matchAll(/\*\*(\S+?)\*\*/g)].map(
        (m) => m[1],
      );
      for (const ref of refs) {
        if (!skills.includes(ref)) {
          error(`${skillName}: broken cross-reference to '${ref}'`);
        }
      }
    }
  }

  // Check plugin.json
  try {
    const pluginRaw = await readFile(PLUGIN_JSON, "utf-8");
    const plugin = JSON.parse(pluginRaw);
    if (!plugin.name) error("plugin.json: missing 'name'");
    if (!plugin.version) error("plugin.json: missing 'version'");
    if (!plugin.skills) error("plugin.json: missing 'skills' path");
    info(`  plugin.json: v${plugin.version} ✓`);
  } catch (e: any) {
    error(`plugin.json: ${e.message}`);
  }

  // Check root marketplace.json references this plugin
  try {
    const marketRaw = await readFile(
      ".claude-plugin/marketplace.json",
      "utf-8",
    );
    const market = JSON.parse(marketRaw);
    const hasSkills = market.plugins?.some(
      (p: any) => p.name === "gram-skills",
    );
    if (hasSkills) {
      info("  marketplace.json: gram-skills entry found ✓");
    } else {
      error("marketplace.json: missing gram-skills entry in plugins array");
    }
  } catch (e: any) {
    error(`marketplace.json: ${e.message}`);
  }
}

// ─────────────────────────────────────────────
// Step 3: Version bump
// ─────────────────────────────────────────────

async function bumpVersion() {
  if (!bumpLevel) return;

  info(chalk.bold("\n● Bumping plugin version\n"));

  const valid = ["patch", "minor", "major"];
  if (!valid.includes(bumpLevel)) {
    error(`Invalid bump level '${bumpLevel}'. Must be: ${valid.join(", ")}`);
    return;
  }

  const pluginRaw = await readFile(PLUGIN_JSON, "utf-8");
  const plugin = JSON.parse(pluginRaw);
  const current = plugin.version || "0.0.0";

  const parts = current.split(".").map(Number);
  if (parts.length !== 3 || parts.some(isNaN)) {
    error(`Invalid current version '${current}' in plugin.json`);
    return;
  }

  let [major, minor, patch] = parts;

  switch (bumpLevel) {
    case "major":
      major++;
      minor = 0;
      patch = 0;
      break;
    case "minor":
      minor++;
      patch = 0;
      break;
    case "patch":
      patch++;
      break;
  }

  const newVersion = `${major}.${minor}.${patch}`;
  plugin.version = newVersion;

  await writeFile(PLUGIN_JSON, JSON.stringify(plugin, null, 2) + "\n");
  fix(`Bumped version: ${current} → ${newVersion}`);
}

// ─────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────

async function run() {
  console.log(chalk.bold.blue("\nGram Agent Skills Update\n"));

  // Step 1: Sync CLI help
  const commands = await captureCliHelp();
  if (commands.size > 0) {
    info(`  Captured help for ${commands.size} commands`);
    await syncCliHelp(commands);
  } else {
    warn("Could not capture any CLI help (is 'gram' installed?)");
  }

  // Step 2: Validate
  await validateStructure();

  // Step 3: Version bump
  await bumpVersion();

  // Summary
  console.log(chalk.bold("\n● Summary\n"));
  const { errors, warnings, fixes } = result;

  if (fixes.length > 0)
    console.log(chalk.green(`  ${fixes.length} fix(es) applied`));
  if (warnings.length > 0)
    console.log(chalk.yellow(`  ${warnings.length} warning(s)`));
  if (errors.length > 0) console.log(chalk.red(`  ${errors.length} error(s)`));
  if (errors.length === 0 && warnings.length === 0) {
    console.log(chalk.green("  All checks passed!"));
  }

  console.log();
  process.exit(errors.length > 0 ? 1 : 0);
}

run();
