#!/usr/bin/env node

//MISE dir="{{ config_root }}"
//MISE hide=true
//MISE description="Configure this worktree's Platform MCP for coding harnesses"

//USAGE flag "--server-url <url>" env="GRAM_SERVER_URL" required_unless="--remove" help="Worktree server URL"
//USAGE flag "--out-file <file>" default=".opencode/opencode.jsonc" help="Local OpenCode config to update"
//USAGE flag "--remove" help="Remove external MCP registrations before deleting this worktree"

import assert from "node:assert/strict";
import { mkdir, readFile, realpath, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { parseJSONC, type JSONCParseError } from "confbox";
import { $ } from "zx";

interface MCPServer {
  name: string;
  url: string;
}

async function claudeAvailable() {
  return (await $`command -v claude`.quiet().nothrow()).exitCode === 0;
}

async function localClaudeServers() {
  const file = join(
    process.env["CLAUDE_CONFIG_DIR"] ?? homedir(),
    ".claude.json",
  );
  let source: string;
  try {
    source = await readFile(file, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return {};
    throw error;
  }
  const config = JSON.parse(source) as {
    projects?: Record<
      string,
      { mcpServers?: Record<string, Record<string, unknown>> }
    >;
  };
  return config.projects?.[await realpath(process.cwd())]?.mcpServers ?? {};
}

async function setupClaudeCode(servers: MCPServer[]) {
  if (!(await claudeAvailable())) return;
  const existing = await localClaudeServers();
  for (const server of servers) {
    const current = existing[server.name];
    if (current?.type === "http" && current.url === server.url) continue;
    if (!current) {
      await $`claude mcp add --scope local --transport http ${server.name} ${server.url}`;
      continue;
    }

    // The CLI rejects duplicate names. Preserve custom options when replacing
    // an old endpoint, and restore the original registration if adding fails.
    const updated = JSON.stringify({
      ...current,
      type: "http",
      url: server.url,
    });
    await $`claude mcp remove --scope local ${server.name}`;
    try {
      await $`claude mcp add-json --scope local ${server.name} ${updated}`;
    } catch (error) {
      await $`claude mcp add-json --scope local ${server.name} ${JSON.stringify(current)}`.nothrow();
      throw error;
    }
  }
}

async function cleanupClaudeCode(names: string[]) {
  if (!(await claudeAvailable())) return;
  const existing = await localClaudeServers();
  for (const name of names) {
    if (!existing[name]) continue;
    const result = await $`claude mcp remove --scope local ${name}`.nothrow();
    if (result.exitCode !== 0) {
      console.warn(`Could not remove local Claude MCP registration: ${name}`);
    }
  }
}

async function setupOpenCode(servers: MCPServer[], file: string) {
  let source = "{}";
  try {
    source = await readFile(file, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }

  const errors: JSONCParseError[] = [];
  const config = parseJSONC<{
    $schema?: string;
    mcp?: { servers?: Record<string, Record<string, unknown>> };
  }>(source, { errors, allowTrailingComma: true });
  assert.equal(errors.length, 0, `Invalid JSONC in ${file}`);
  assert(
    config && typeof config === "object" && !Array.isArray(config),
    "config must be an object",
  );
  config.$schema ??= "https://opencode.ai/config.json";
  config.mcp ??= {};
  config.mcp.servers ??= {};
  for (const server of servers) {
    config.mcp.servers[server.name] = {
      ...config.mcp.servers[server.name],
      type: "remote",
      url: server.url,
    };
  }

  await mkdir(dirname(file), { recursive: true });
  await writeFile(file, JSON.stringify(config, null, 2) + "\n");
  for (const server of servers) {
    console.log(`✅ Registered ${server.name} MCP at ${server.url} in ${file}`);
  }
}

async function main() {
  const definitions = [{ name: "platform", path: "/platform-mcp" }];
  if (process.env["usage_remove"] === "true") {
    // OpenCode's ignored config disappears with the worktree; Claude's lives
    // outside it, so remove only the local servers managed by this task.
    await cleanupClaudeCode(definitions.map((server) => server.name));
    return;
  }
  const serverURL = process.env["usage_server_url"];
  assert(serverURL, "server URL is required");
  const url = new URL(serverURL);
  assert(
    ["http:", "https:"].includes(url.protocol),
    "server URL must use HTTP(S)",
  );
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
