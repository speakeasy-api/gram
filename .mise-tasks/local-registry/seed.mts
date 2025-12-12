#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Seed the local MCP registry with toolsets from a Gram project"

//USAGE flag "--project <slug>" required=#true help="Project slug to fetch toolsets from"
//USAGE flag "--registry-url <url>" env="LOCAL_MCP_REGISTRY_URL" required=#true help="URL of the local MCP registry"
//USAGE flag "--api-url <url>" env="GRAM_SERVER_URL" required=#true help="Gram API URL"
//USAGE flag "--api-key <key>" env="GRAM_API_KEY" required=#true help="Gram API key"

import assert from "node:assert";
import { Gram } from "@gram/client";

async function fetchRegistryToken(registryUrl: string): Promise<string> {
  console.log(`Authenticating with registry...`);
  const res = await fetch(`${registryUrl}/v0.1/auth/none`, {
    method: "POST",
  });
  if (!res.ok) {
    const text = await res.text();
    console.error(`Failed to get registry token: ${res.status} ${res.statusText}`);
    console.error(text);
    process.exit(1);
  }
  const body = await res.json() as { registry_token: string; expires_at: number };
  console.log(`Got registry token (expires ${new Date(body.expires_at * 1000).toISOString()})`);
  return body.registry_token;
}


async function run() {
  const projectSlug = process.env["usage_project"];
  assert(projectSlug, "Project slug is required");

  const registryUrl = process.env["usage_registry_url"];
  assert(registryUrl, "Registry URL is required");

  const gramApiUrl = process.env["usage_api_url"];
  assert(gramApiUrl, "Gram API URL is required");

  const gramApiKey = process.env["usage_api_key"];
  assert(gramApiKey, "Gram API key is required");

  // Check registry is reachable
  try {
    const healthRes = await fetch(`${registryUrl}/v0.1/health`);
    if (!healthRes.ok) {
      throw new Error(`Health check failed: ${healthRes.status}`);
    }
  } catch (error) {
    console.error(`Cannot connect to registry at ${registryUrl}`);
    console.error("Make sure to run: mise local-registry:start");
    process.exit(1);
  }

  const gram = new Gram({
    serverURL: gramApiUrl,
  });

  console.log(`Fetching toolsets for project: ${projectSlug}`);

  const result = await gram.toolsets.list({
    gramProject: projectSlug,
    gramKey: gramApiKey,
  });

  const toolsets = result.toolsets.filter(t => t.mcpEnabled);

  if (toolsets.length === 0) {
    console.log("No MCP-enabled toolsets found in this project.");
    return;
  }

  console.log(`Found ${toolsets.length} MCP-enabled toolset(s)\n`);
  console.log(`Seeding registry at ${registryUrl}\n`);

  const registryToken = await fetchRegistryToken(registryUrl);

  for (const toolset of toolsets) {
    const mcpSlug = toolset.mcpSlug || toolset.slug;
    const serverName = `io.modelcontextprotocol.anonymous/${mcpSlug}`;
    const mcpUrl = `${gramApiUrl}/mcp/${projectSlug}/${mcpSlug}`;

    const serverDef = {
      $schema: "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
      name: serverName,
      description: toolset.description || `${toolset.name} MCP Server`,
      version: "1.0.0",
      remotes: [
        {
          type: "streamable-http" as const,
          url: mcpUrl,
        },
      ],
    };

    console.log(`Publishing: ${serverName}`);
    console.log(`  URL: ${mcpUrl}`);

    try {
      const res = await fetch(`${registryUrl}/v0.1/publish`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Authorization": `Bearer ${registryToken}`,
        },
        body: JSON.stringify(serverDef),
      });

      if (!res.ok) {
        const errorText = await res.text();
        console.error(`  Failed: ${res.status} ${res.statusText}`);
        console.error(`  ${errorText}`);
        continue;
      }

      const result = await res.json();
      console.log(`  Success!`);
      if (result.id) {
        console.log(`  ID: ${result.id}`);
      }
    } catch (error) {
      console.error(`  Error: ${error}`);
    }

    console.log("");
  }

  console.log("Done!");
}

run();
