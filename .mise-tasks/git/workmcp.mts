#!/usr/bin/env node

//MISE dir="{{ config_root }}"
//MISE hide=true
//MISE description="Configure this worktree's Platform MCP for coding harnesses"

//USAGE flag "--server-url <url>" env="GRAM_SERVER_URL" required_unless="--remove" help="Worktree server URL"
//USAGE flag "--out-file <file>" default=".opencode/opencode.jsonc" help="Local OpenCode config to update"
//USAGE flag "--remove" help="Remove external MCP registrations before deleting this worktree"
//USAGE flag "--worktree <dir>" help="Target worktree for --remove (defaults to this worktree)"

import assert from "node:assert/strict";
import { mkdir, readFile, realpath, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { applyEdits, modify, parse, type ParseError } from "jsonc-parser";
import { $ } from "zx";

interface MCPServer {
  name: string;
  url: string;
}

async function claudeExecutable() {
  const result = await $`command -v claude`.quiet().nothrow();
  return result.exitCode === 0 ? resolve(result.stdout.trim()) : undefined;
}

async function readJSON<T>(file: string, fallback: T): Promise<T> {
  try {
    return JSON.parse(await readFile(file, "utf8")) as T;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return fallback;
    throw error;
  }
}

async function ownershipFile(worktree: string) {
  const gitDir = (
    await $`git -C ${worktree} rev-parse --absolute-git-dir`.quiet()
  ).stdout.trim();
  return join(gitDir, "gram-workmcp.json");
}

async function localClaudeServers(worktree: string) {
  const file = join(
    process.env["CLAUDE_CONFIG_DIR"] ?? homedir(),
    ".claude.json",
  );
  const config = await readJSON<{
    projects?: Record<
      string,
      { mcpServers?: Record<string, Record<string, unknown>> }
    >;
  }>(file, {});
  return config.projects?.[worktree]?.mcpServers ?? {};
}

async function setupClaudeCode(servers: MCPServer[]) {
  const claude = await claudeExecutable();
  if (!claude) return;
  const worktree = await realpath(process.cwd());
  const existing = await localClaudeServers(worktree);
  const file = await ownershipFile(worktree);
  const owned = await readJSON<Record<string, string>>(file, {});
  for (const server of servers) {
    const current = existing[server.name];
    if (current?.type === "http" && current.url === server.url) continue;
    if (!current) {
      await $`${claude} mcp add --scope local --transport http ${server.name} ${server.url}`;
      owned[server.name] = server.url;
      await writeFile(file, JSON.stringify(owned, null, 2) + "\n");
      continue;
    }

    // The CLI rejects duplicate names. Preserve custom options when replacing
    // an old endpoint, and restore the original registration if adding fails.
    const updated = JSON.stringify({
      ...current,
      type: "http",
      url: server.url,
    });
    await $`${claude} mcp remove --scope local ${server.name}`;
    try {
      await $`${claude} mcp add-json --scope local ${server.name} ${updated}`;
    } catch (error) {
      await $`${claude} mcp add-json --scope local ${server.name} ${JSON.stringify(current)}`.nothrow();
      throw error;
    }
    // Only newly created registrations are ours to remove. A pre-existing
    // registration can be updated without transferring its ownership.
    if (Object.hasOwn(owned, server.name)) {
      owned[server.name] = server.url;
      await writeFile(file, JSON.stringify(owned, null, 2) + "\n");
    }
  }
}

async function cleanupClaudeCode(names: string[], worktree: string) {
  const claude = await claudeExecutable();
  if (!claude) return;
  const existing = await localClaudeServers(worktree);
  const file = await ownershipFile(worktree);
  const owned = await readJSON<Record<string, string>>(file, {});
  const run = $({ cwd: worktree });
  let failed = false;
  for (const name of names) {
    if (!Object.hasOwn(owned, name)) continue;
    if (
      existing[name]?.type !== "http" ||
      existing[name]?.url !== owned[name]
    ) {
      delete owned[name];
      continue;
    }
    const result =
      await run`${claude} mcp remove --scope local ${name}`.nothrow();
    if (result.exitCode !== 0) {
      console.warn(
        `Could not remove local Claude MCP registration ${name} for ${worktree}`,
      );
      failed = true;
    } else {
      delete owned[name];
    }
  }
  await writeFile(file, JSON.stringify(owned, null, 2) + "\n");
  assert(!failed, `MCP cleanup failed for ${worktree}`);
}

async function setupOpenCode(servers: MCPServer[], file: string) {
  let source = "{}";
  try {
    source = await readFile(file, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }

  const errors: ParseError[] = [];
  const config = parse(source, errors, { allowTrailingComma: true });
  assert.equal(errors.length, 0, `Invalid JSONC in ${file}`);
  assert(
    config && typeof config === "object" && !Array.isArray(config),
    "config must be an object",
  );
  // Edit the JSONC text in place: confbox can parse it, but a parse/stringify
  // round trip would discard the user's comments and trailing commas.
  const set = (path: string[], value: unknown) => {
    source = applyEdits(
      source,
      modify(source, path, value, {
        formattingOptions: { insertSpaces: true, tabSize: 2 },
      }),
    );
  };
  if (!config.$schema) set(["$schema"], "https://opencode.ai/config.json");
  // OpenCode V2 uses mcp.servers, unlike the legacy V1 server map.
  // https://opencode.ai/v2/docs/mcp-servers#config
  for (const server of servers) {
    set(["mcp", "servers", server.name, "type"], "remote");
    set(["mcp", "servers", server.name, "url"], server.url);
  }

  await mkdir(dirname(file), { recursive: true });
  await writeFile(file, source.endsWith("\n") ? source : source + "\n");
  for (const server of servers) {
    console.log(`✅ Registered ${server.name} MCP at ${server.url} in ${file}`);
  }
}

async function main() {
  const definitions = [{ name: "platform", path: "/platform-mcp" }];
  if (process.env["usage_remove"] === "true") {
    // OpenCode's ignored config disappears with the worktree; Claude's lives
    // outside it, so remove only the local servers managed by this task.
    await cleanupClaudeCode(
      definitions.map((server) => server.name),
      await realpath(process.env["usage_worktree"] || process.cwd()),
    );
    return;
  }
  assert(
    !process.env["usage_worktree"],
    "--worktree is only supported with --remove",
  );
  const serverURL = process.env["usage_server_url"];
  assert(serverURL, "server URL is required");
  const url = new URL(serverURL);
  assert(url.protocol === "https:", "server URL must use HTTPS");
  const servers: MCPServer[] = definitions.map((server) => ({
    name: server.name,
    url: new URL(server.path, url).href,
  }));

  await setupOpenCode(
    servers,
    process.env["usage_out_file"] ?? ".opencode/opencode.jsonc",
  );
  await setupClaudeCode(servers);
}

await main();
