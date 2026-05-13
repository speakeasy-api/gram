#!/usr/bin/env -S node

//MISE description="Seed the local database with data"

import assert from "node:assert";
import { createServer } from "node:http";
import crypto from "node:crypto";
import { exec } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { intro, log, outro } from "@clack/prompts";
import { GramCore } from "@gram/client/core.js";
import { assetsUploadFunctions } from "@gram/client/funcs/assetsUploadFunctions.js";
import { assetsUploadOpenAPIv3 } from "@gram/client/funcs/assetsUploadOpenAPIv3.js";
import { authInfo } from "@gram/client/funcs/authInfo.js";
import { deploymentsEvolveDeployment } from "@gram/client/funcs/deploymentsEvolveDeployment.js";
import { deploymentsGetById } from "@gram/client/funcs/deploymentsGetById.js";
import { keysCreate } from "@gram/client/funcs/keysCreate.js";
import { keysList } from "@gram/client/funcs/keysList.js";
import { keysRevokeById } from "@gram/client/funcs/keysRevokeById.js";
import { keysValidate } from "@gram/client/funcs/keysValidate.js";
import { projectsCreate } from "@gram/client/funcs/projectsCreate.js";
import { projectsRead } from "@gram/client/funcs/projectsRead.js";
import { resourcesList } from "@gram/client/funcs/resourcesList.js";
import { toolsList } from "@gram/client/funcs/toolsList.js";
import { toolsetsCreate } from "@gram/client/funcs/toolsetsCreate.js";
import { toolsetsUpdateBySlug } from "@gram/client/funcs/toolsetsUpdateBySlug.js";
import { environmentsCreate } from "@gram/client/funcs/environmentsCreate.js";
import { environmentsList } from "@gram/client/funcs/environmentsList.js";
import { ServiceError } from "@gram/client/models/errors";
import { $ } from "zx";

type Asset = {
  slug: string;
} & (
  | ({
      type: "openapi";
      storybookDefault?: boolean;
    } & ({ filename: string } | { url: string }))
  | {
      type: "functions";
      runtime: "nodejs:22" | "nodejs:24";
      resourceUris: string[];
    }
);

const PLAYGROUND_MCP_APP_SLUG = "playground-mcp-app";
const PLAYGROUND_MCP_APP_TOOL_NAME = "show_dashboard";
const PLAYGROUND_MCP_APP_RESOURCE_URI = `ui://${PLAYGROUND_MCP_APP_SLUG}/dashboard`;

const SEED_PROJECTS: {
  name: string;
  slug: string;
  summary: string;
  mcpPublic: boolean;
  assets: Asset[];
}[] = [
  {
    name: "E-Commerce API",
    slug: "ecommerce-api",
    summary: "A mock e-commerce API to allow working with Gram Elements",
    mcpPublic: true,
    assets: [
      {
        type: "openapi",
        slug: "ecommerce-api",
        url: "https://gram-mcp-storybook.vercel.app/openapi",
        storybookDefault: true,
      },
      {
        type: "openapi",
        slug: "gram",
        filename: path.join("server", "gen", "http", "openapi3.yaml"),
      },
      {
        type: "functions",
        slug: PLAYGROUND_MCP_APP_SLUG,
        runtime: "nodejs:22",
        resourceUris: [PLAYGROUND_MCP_APP_RESOURCE_URI],
      },
    ],
  },
];

async function authenticateViaMockIDP(serverURL: string): Promise<string> {
  const idpAddress = process.env["SPEAKEASY_SERVER_ADDRESS"];
  if (!idpAddress) {
    throw new Error("SPEAKEASY_SERVER_ADDRESS is not set");
  }

  const secretKey = process.env["SPEAKEASY_SECRET_KEY"];
  if (!secretKey) {
    throw new Error("SPEAKEASY_SECRET_KEY is not set");
  }

  // Step 1: Hit the mock IDP login endpoint to get an auth code.
  // Use a dummy return_url — we only need the code from the redirect Location.
  const loginURL = `${idpAddress}/v1/speakeasy_provider/login?return_url=http://localhost/callback`;
  const loginRes = await fetch(loginURL, { redirect: "manual" });
  const location = loginRes.headers.get("location");
  if (!location) {
    throw new Error("Mock IDP login did not return a redirect");
  }

  // Check if the redirect contains a code (mock mode) or points elsewhere (OIDC mode).
  const redirectUrl = new URL(location);
  const code = redirectUrl.searchParams.get("code");

  if (code) {
    // Mock mode: the IDP returned a code directly.
    return exchangeCodeWithServer(serverURL, code);
  }

  // OIDC mode: the IDP redirected to an external provider (e.g. WorkOS).
  // We need a browser-based flow to complete authentication.
  log.info("OIDC mode detected — opening browser for authentication...");
  return authenticateViaBrowser(serverURL, idpAddress);
}

/**
 * Opens a browser for the user to complete OIDC authentication.
 * Starts a temporary local HTTP server to capture the redirect code.
 */
async function authenticateViaBrowser(
  serverURL: string,
  idpAddress: string,
): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    const server = createServer((req, res) => {
      const url = new URL(req.url!, `http://localhost`);
      const code = url.searchParams.get("code");

      if (!code) {
        res.writeHead(400, { "Content-Type": "text/html" });
        res.end("<h1>Error</h1><p>No code received. Please try again.</p>");
        return;
      }

      res.writeHead(200, { "Content-Type": "text/html" });
      res.end(
        "<h1>Authenticated!</h1><p>You can close this tab and return to the terminal.</p>",
      );

      server.close();

      exchangeCodeWithServer(serverURL, code).then(resolve).catch(reject);
    });

    // Listen on a random available port
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      if (!addr || typeof addr === "string") {
        reject(new Error("Failed to start local callback server"));
        return;
      }
      const callbackUrl = `http://127.0.0.1:${addr.port}/callback`;
      const loginURL = `${idpAddress}/v1/speakeasy_provider/login?return_url=${encodeURIComponent(callbackUrl)}`;

      // Open the browser
      const openCmd =
        process.platform === "darwin"
          ? "open"
          : process.platform === "win32"
            ? "start"
            : "xdg-open";
      exec(`${openCmd} '${loginURL}'`, (err) => {
        if (err) {
          log.warn(
            `Could not open browser automatically. Please visit:\n${loginURL}`,
          );
        }
      });
    });

    // Timeout after 2 minutes (unref so it doesn't block exit)
    const timeout = setTimeout(() => {
      server.close();
      reject(new Error("Authentication timed out after 2 minutes"));
    }, 120_000);
    timeout.unref();
  });
}

/** Exchange an auth code with the Gram server's callback endpoint. */
async function exchangeCodeWithServer(
  serverURL: string,
  code: string,
): Promise<string> {
  const callbackURL = `${serverURL}/rpc/auth.callback?code=${encodeURIComponent(code)}`;
  const callbackRes = await fetch(callbackURL, { redirect: "manual" });
  const sessionToken = callbackRes.headers.get("gram-session");
  if (!sessionToken) {
    throw new Error(
      `Server callback did not return a session (status=${callbackRes.status})`,
    );
  }
  return sessionToken;
}

async function seed() {
  let success = false;
  intro("Seeding local development environment...");
  using _ = {
    [Symbol.dispose]() {
      outro(success ? "Seeding complete!" : "Seeding failed.");
    },
  };
  const serverURL = process.env["GRAM_SERVER_URL"];
  if (!serverURL) {
    throw new Error("GRAM_SERVER_URL is not set");
  }
  const functionsProvider = process.env["GRAM_FUNCTIONS_PROVIDER"] ?? "local";
  const shouldSeedFunctions = functionsProvider === "local";
  if (!shouldSeedFunctions) {
    log.info(
      `Skipping seeded MCP app function assets because GRAM_FUNCTIONS_PROVIDER is '${functionsProvider}', not 'local'.`,
    );
  }

  const gram = new GramCore({ serverURL });

  // Authenticate via the mock IDP to get a session token.
  log.info("Authenticating via mock IDP...");
  const sessionId = await authenticateViaMockIDP(serverURL);
  log.info("Authenticated successfully.");

  const res = await authInfo(gram, undefined, {
    sessionHeaderGramSession: sessionId,
  });
  if (!res.ok) {
    abort("Failed to query session info", res.error);
  }
  const sessionInfo = res.value;
  const sessionJSON = JSON.stringify(sessionInfo, null, 2);

  const activeOrgID = sessionInfo.result.activeOrganizationId;
  if (!activeOrgID) {
    abort("Active organization ID not found", sessionJSON);
  }

  const orgs = sessionInfo.result.organizations;
  const org = orgs.find(
    (o: unknown) =>
      typeof o === "object" && o != null && "id" in o && o?.id === activeOrgID,
  );
  if (!org) {
    abort("Active organization not found", sessionJSON);
  }

  const projects: Record<string, { slug: string; id: string }> = {};
  for (const p of org.projects) {
    const id = p.id;
    const slug = p.slug;
    projects[slug] = { id, slug };
  }

  // Seed the default MCP registry (Pulse)
  try {
    const dbUser = process.env.DB_USER || "gram";
    const dbName = process.env.DB_NAME || "gram";
    await $`docker compose exec gram-db psql -U ${dbUser} -d ${dbName} -c "INSERT INTO mcp_registries (name, url) VALUES ('Gram Recommended', 'https://api.pulsemcp.com') ON CONFLICT (url) WHERE deleted IS FALSE DO NOTHING;"`.quiet();
    log.info("Seeded MCP registry 'Gram Recommended'");
  } catch (e: unknown) {
    const err = e as { stderr?: string; message?: string };
    log.warn(
      `Failed to seed MCP registry: ${err.message || err.stderr || JSON.stringify(e)}`,
    );
  }

  // oxlint-disable-next-line no-unused-vars
  const key = await initAPIKey({
    gram,
    sessionId,
  });

  // Collect all tool URNs per project for seeding observability data
  const projectToolUrns: Record<string, string[]> = {};

  for (const { name, slug, assets, mcpPublic } of SEED_PROJECTS) {
    const seedAssets = shouldSeedFunctions
      ? assets
      : assets.filter((asset) => asset.type !== "functions");

    const {
      created,
      id,
      slug: projectSlug,
    } = await getOrCreateProject({
      gram,
      sessionId,
      activeOrgID,
      slug,
    });
    projects[projectSlug] = { id, slug: projectSlug };
    projectToolUrns[projectSlug] = [];
    let verb = created ? "Created" : "Found existing";
    log.info(`${verb} project '${projectSlug}' (project_id = ${id})`);

    if (seedAssets.length === 0) {
      log.info(`No seed assets selected for '${projectSlug}', skipping.`);
      continue;
    }

    const deploymentId = await deployAssets({
      gram,
      sessionId,
      projectSlug,
      projectName: name,
      assets: seedAssets,
    });
    log.info(
      `Deployed assets into '${projectSlug}' (deployment_id = ${deploymentId})`,
    );

    for (const asset of seedAssets) {
      const toolset = await upsertToolset({
        gram,
        serverURL,
        sessionId,
        projectSlug,
        deploymentId,
        asset,
        mcpPublic,
      });
      verb = toolset.created ? "Created" : "Updated";
      log.info(
        `${verb} toolset '${toolset.slug}' for project '${projectSlug}' (mcp_url = ${toolset.mcpURL}, tools: ${toolset.toolUrns.length})`,
      );

      // Collect tool URNs for observability seeding
      projectToolUrns[projectSlug].push(...toolset.toolUrns);

      if (asset.type === "openapi" && asset.storybookDefault) {
        await $`mise set --file mise.local.toml \
        VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG=${projectSlug} \
        VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL=${toolset.mcpURL}`;
      }
    }
  }

  // Create the MCP Logs toolset — a curated subset of Gram's own API tools
  // exposed as a built-in MCP server on the project MCP page.
  // In production this lives in speakeasy-team/kitchen-sink; locally we
  // reuse ecommerce-api since the gram asset is already deployed there.
  {
    const projectSlug = SEED_PROJECTS[0].slug;
    const mcpLogsToolset = await upsertMcpLogsToolset({
      gram,
      serverURL,
      sessionId,
      projectSlug,
    });
    const verb = mcpLogsToolset.created ? "Created" : "Updated";
    log.info(
      `${verb} MCP Logs toolset '${mcpLogsToolset.slug}' for project '${projectSlug}' (mcp_url = ${mcpLogsToolset.mcpURL})`,
    );

    await $`mise set --file mise.local.toml \
      VITE_GRAM_OBSERVABILITY_MCP_URL=${mcpLogsToolset.mcpURL}`;
    log.info(`Set VITE_GRAM_OBSERVABILITY_MCP_URL in mise.local.toml`);
  }

  // Seed a default environment for each project
  for (const { slug: projectSlug } of SEED_PROJECTS) {
    const env = await getOrCreateEnvironment({
      gram,
      sessionId,
      projectSlug,
      activeOrgID,
      name: "Default",
    });
    log.info(
      `${env.created ? "Created" : "Found existing"} environment '${env.slug}' for project '${projectSlug}'`,
    );
  }

  // Seed observability data for the first seeded project
  const firstSeededProjectSlug = Object.keys(projectToolUrns)[0];
  const firstProject = firstSeededProjectSlug
    ? projects[firstSeededProjectSlug]
    : undefined;
  if (firstProject) {
    const toolUrns = projectToolUrns[firstProject.slug] ?? [];
    await seedObservabilityData({
      projectId: firstProject.id,
      organizationId: activeOrgID,
      toolUrns,
    });
  }

  success = true;
}

async function initAPIKey(init: {
  gram: GramCore;
  sessionId: string;
}): Promise<void> {
  const { gram, sessionId } = init;

  const existing = process.env["GRAM_API_KEY"];
  if (existing) {
    const vres = await keysValidate(gram, undefined, {
      apikeyHeaderGramKey: existing,
    });
    if (vres.ok) {
      log.info(`Using existing GRAM_API_KEY environment variable.`);
      return;
    }
    log.warn(`Existing GRAM_API_KEY is invalid. Creating a new API key...`);
  }

  // Revoke any existing seed-key before creating a new one
  const listRes = await keysList(gram, undefined, {
    sessionHeaderGramSession: sessionId,
  });
  if (listRes.ok) {
    const existingKey = listRes.value.keys.find((k) => k.name === "seed-key");
    if (existingKey) {
      log.info(`Revoking existing seed-key...`);
      await keysRevokeById(
        gram,
        { id: existingKey.id },
        { sessionHeaderGramSession: sessionId },
      );
    }
  }

  const keyRes = await keysCreate(
    gram,
    {
      createKeyForm: { name: "seed-key", scopes: ["producer", "chat"] },
    },
    {
      sessionHeaderGramSession: sessionId,
    },
  );
  if (!keyRes.ok) {
    abort(`Failed to create API key 'seed-key'`, keyRes.error);
  }

  const apiKey = keyRes.value.key;
  assert(keyRes.value.key, "API key not found in /rpc/keys.create response");
  await $`mise set --file mise.local.toml GRAM_API_KEY=${apiKey}`;
  log.info(
    `Created new API key and set GRAM_API_KEY environment variable in mise.local.toml.`,
  );
}

async function getOrCreateEnvironment(init: {
  gram: GramCore;
  sessionId: string;
  projectSlug: string;
  activeOrgID: string;
  name: string;
}): Promise<{ created: boolean; slug: string }> {
  const { gram, sessionId, projectSlug, activeOrgID, name } = init;

  // Check if environment already exists
  const listRes = await environmentsList(gram, undefined, {
    sessionHeaderGramSession: sessionId,
    projectSlugHeaderGramProject: projectSlug,
  });
  if (listRes.ok) {
    const existing = listRes.value.environments.find((e) => e.name === name);
    if (existing) {
      return { created: false, slug: existing.slug };
    }
  }

  // Create the environment
  const res = await environmentsCreate(
    gram,
    {
      createEnvironmentForm: {
        organizationId: activeOrgID,
        name,
        entries: [],
      },
    },
    {
      sessionHeaderGramSession: sessionId,
      projectSlugHeaderGramProject: projectSlug,
    },
  );
  if (!res.ok) {
    abort(`Failed to create environment '${name}'`, res.error);
  }

  return { created: true, slug: res.value.slug };
}

async function getOrCreateProject(init: {
  gram: GramCore;
  sessionId: string;
  activeOrgID: string;
  slug: string;
}): Promise<{ created: boolean; id: string; slug: string }> {
  const { gram, sessionId, activeOrgID, slug } = init;
  const res = await projectsCreate(
    gram,
    {
      createProjectRequestBody: {
        organizationId: activeOrgID,
        name: slug,
      },
    },
    {
      sessionHeaderGramSession: sessionId,
    },
  );
  switch (true) {
    case !res.ok &&
      res.error instanceof ServiceError &&
      res.error.data$.name === "conflict":
      const getRes = await projectsRead(
        gram,
        { slug },
        { sessionHeaderGramSession: sessionId },
      );
      if (!getRes.ok) {
        abort(`Failed to get existing project \`${slug}\``, getRes.error);
      }
      return {
        created: false,
        id: getRes.value.project.id,
        slug: getRes.value.project.slug,
      };
    case !res.ok:
      abort(`Failed to create project \`${slug}\``, res.error);
    default:
      return {
        created: true,
        id: res.value.project.id,
        slug: res.value.project.slug,
      };
  }
}

async function deployAssets(init: {
  gram: GramCore;
  sessionId: string;
  projectSlug: string;
  projectName: string;
  assets: Asset[];
}): Promise<string> {
  const { sessionId, projectSlug, projectName, assets } = init;

  const oapi: Array<{ assetId: string; name: string; slug: string }> = [];
  const functions: Array<{
    assetId: string;
    name: string;
    runtime: string;
    slug: string;
  }> = [];

  for (const asset of assets) {
    if (asset.type === "openapi") {
      let spec: string;
      let contentType: string;

      if ("url" in asset) {
        const response = await fetch(asset.url);
        if (!response.ok) {
          abort(
            `Failed to fetch OpenAPI spec from ${asset.url}`,
            response.statusText,
          );
        }
        spec = await response.text();
        contentType = "application/json";
      } else {
        spec = await fs.readFile(asset.filename, "utf-8");
        contentType = asset.filename.endsWith(".yaml")
          ? "application/x-yaml"
          : "application/json";
      }

      const requestBody = new Blob([spec], { type: contentType });
      const res = await assetsUploadOpenAPIv3(
        init.gram,
        {
          contentLength: requestBody.size,
          requestBody,
        },
        {
          option2: {
            projectSlugHeaderGramProject: projectSlug,
            sessionHeaderGramSession: sessionId,
          },
        },
      );

      if (!res.ok) {
        const source = "url" in asset ? asset.url : asset.filename;
        abort(`Failed to upload asset \`${source}\``, res.error);
      }

      const { id: assetId } = await res.value.asset;
      oapi.push({ assetId, name: asset.slug, slug: asset.slug });
      continue;
    }

    const archive = await buildSeedFunctionArchive(asset);
    const requestBody = new Blob([new Uint8Array(archive)], {
      type: "application/zip",
    });
    const res = await assetsUploadFunctions(
      init.gram,
      {
        contentLength: requestBody.size,
        requestBody,
      },
      {
        option2: {
          projectSlugHeaderGramProject: projectSlug,
          sessionHeaderGramSession: sessionId,
        },
      },
    );

    if (!res.ok) {
      abort(`Failed to upload functions asset \`${asset.slug}\``, res.error);
    }

    const { id: assetId } = await res.value.asset;
    functions.push({
      assetId,
      name: asset.slug,
      runtime: asset.runtime,
      slug: asset.slug,
    });
  }

  const evolveRes = await deploymentsEvolveDeployment(
    init.gram,
    {
      evolveForm: {
        upsertOpenapiv3Assets: oapi,
        upsertFunctions: functions,
      },
    },
    {
      option2: {
        projectSlugHeaderGramProject: projectSlug,
        sessionHeaderGramSession: sessionId,
      },
    },
  );

  if (!evolveRes.ok) {
    abort(`Failed to evolve project \`${projectName}\``, evolveRes.error);
  }

  const deploymentId = evolveRes.value.deployment?.id;
  if (typeof deploymentId !== "string" || !deploymentId) {
    abort("Deployment ID not found", evolveRes.value);
  }

  await waitForDeploymentCompletion({
    deploymentId,
    gram: init.gram,
    projectSlug,
    sessionId,
  });

  return deploymentId;
}

async function waitForDeploymentCompletion(init: {
  deploymentId: string;
  gram: GramCore;
  projectSlug: string;
  sessionId: string;
}): Promise<void> {
  const { deploymentId, gram, projectSlug, sessionId } = init;
  const startedAt = Date.now();
  const timeoutMs = 2 * 60 * 1000;

  while (Date.now() - startedAt < timeoutMs) {
    const res = await deploymentsGetById(
      gram,
      { id: deploymentId },
      {
        option2: {
          projectSlugHeaderGramProject: projectSlug,
          sessionHeaderGramSession: sessionId,
        },
      },
    );
    if (!res.ok) {
      abort(
        `Failed to fetch deployment \`${deploymentId}\` while waiting for completion`,
        res.error,
      );
    }

    const deployment = res.value;

    switch (deployment.status) {
      case "completed":
        return;
      case "failed":
        abort(`Deployment \`${deploymentId}\` failed`, deployment);
    }

    await sleep(1500);
  }

  abort(`Timed out waiting for deployment \`${deploymentId}\` to complete`, {
    deploymentId,
    timeoutMs,
  });
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function buildSeedFunctionArchive(
  asset: Extract<Asset, { type: "functions" }>,
): Promise<Buffer> {
  const tmpDir = await fs.mkdtemp(
    path.join(os.tmpdir(), "gram-seed-functions-"),
  );
  const archivePath = path.join(tmpDir, "bundle.zip");
  const manifestPath = path.join(tmpDir, "manifest.json");
  const functionsPath = path.join(tmpDir, "functions.js");

  try {
    await fs.writeFile(
      manifestPath,
      JSON.stringify(
        {
          version: "0.0.0",
          tools: [
            {
              name: PLAYGROUND_MCP_APP_TOOL_NAME,
              description:
                "Return demo data for the seeded MCP Apps playground example",
              inputSchema: {
                type: "object",
                properties: {
                  query: {
                    type: "string",
                    description:
                      "A short topic to render inside the seeded dashboard",
                  },
                },
                required: ["query"],
                additionalProperties: false,
              },
              meta: {
                "ui/resourceUri": PLAYGROUND_MCP_APP_RESOURCE_URI,
              },
            },
          ],
          resources: [
            {
              name: "playground_dashboard",
              title: "Playground MCP App",
              description:
                "A tiny interactive HTML dashboard for validating MCP Apps in the playground",
              uri: PLAYGROUND_MCP_APP_RESOURCE_URI,
              mimeType: "text/html;profile=mcp-app",
              meta: {
                ui: {
                  prefersBorder: true,
                },
              },
            },
          ],
        },
        null,
        2,
      ),
    );
    await fs.writeFile(functionsPath, buildSeedFunctionsSource(asset.slug));
    await $({
      cwd: tmpDir,
    })`zip -q -j ${archivePath} manifest.json functions.js`;

    return await fs.readFile(archivePath);
  } finally {
    await fs.rm(tmpDir, { recursive: true, force: true });
  }
}

function buildSeedFunctionsSource(functionSlug: string): string {
  const dashboardHtml = String.raw`<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Gram MCP App Demo</title>
    <style>
      :root {
        color-scheme: light dark;
        --bg: #f8fafc;
        --surface: rgba(255, 255, 255, 0.92);
        --surface-strong: #ffffff;
        --border: rgba(15, 23, 42, 0.12);
        --text: #0f172a;
        --muted: #475569;
        --accent: #0f766e;
        --accent-soft: rgba(15, 118, 110, 0.12);
        --shadow: 0 24px 48px rgba(15, 23, 42, 0.12);
      }

      @media (prefers-color-scheme: dark) {
        :root {
          --bg: #020617;
          --surface: rgba(15, 23, 42, 0.92);
          --surface-strong: #0f172a;
          --border: rgba(148, 163, 184, 0.18);
          --text: #e2e8f0;
          --muted: #94a3b8;
          --accent: #2dd4bf;
          --accent-soft: rgba(45, 212, 191, 0.18);
          --shadow: 0 24px 48px rgba(2, 6, 23, 0.5);
        }
      }

      * { box-sizing: border-box; }

      body {
        margin: 0;
        min-height: 100vh;
        font-family: "IBM Plex Sans", "Inter", sans-serif;
        background:
          radial-gradient(circle at top left, rgba(45, 212, 191, 0.18), transparent 32rem),
          linear-gradient(180deg, rgba(15, 23, 42, 0.04), transparent 24rem),
          var(--bg);
        color: var(--text);
      }

      main {
        padding: 18px;
      }

      .shell {
        overflow: hidden;
        border: 1px solid var(--border);
        border-radius: 20px;
        background: var(--surface);
        box-shadow: var(--shadow);
        backdrop-filter: blur(20px);
      }

      .hero {
        display: grid;
        gap: 10px;
        padding: 18px 18px 14px;
        background:
          linear-gradient(135deg, var(--accent-soft), transparent 68%),
          linear-gradient(180deg, rgba(255, 255, 255, 0.3), transparent);
      }

      .eyebrow {
        display: inline-flex;
        align-items: center;
        width: fit-content;
        padding: 6px 10px;
        border-radius: 999px;
        background: rgba(15, 118, 110, 0.12);
        color: var(--accent);
        font-size: 12px;
        font-weight: 700;
        letter-spacing: 0.08em;
        text-transform: uppercase;
      }

      h1 {
        margin: 0;
        font-size: 24px;
        line-height: 1.1;
      }

      .lede {
        margin: 0;
        color: var(--muted);
        font-size: 14px;
        line-height: 1.5;
      }

      .grid {
        display: grid;
        gap: 12px;
        padding: 0 18px 18px;
      }

      .panel {
        border: 1px solid var(--border);
        border-radius: 16px;
        background: var(--surface-strong);
        padding: 14px;
      }

      .panel h2 {
        margin: 0 0 10px;
        font-size: 13px;
        letter-spacing: 0.04em;
        text-transform: uppercase;
      }

      .value {
        font-size: 28px;
        font-weight: 700;
        line-height: 1;
      }

      .muted {
        color: var(--muted);
        font-size: 13px;
      }

      pre {
        margin: 0;
        white-space: pre-wrap;
        word-break: break-word;
        font-family: "IBM Plex Mono", "SFMono-Regular", monospace;
        font-size: 12px;
        line-height: 1.55;
      }

      .timeline {
        display: grid;
        gap: 10px;
      }

      .timeline-item {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: 10px;
        align-items: start;
      }

      .dot {
        width: 10px;
        height: 10px;
        margin-top: 5px;
        border-radius: 999px;
        background: var(--accent);
        box-shadow: 0 0 0 6px var(--accent-soft);
      }
    </style>
  </head>
  <body>
    <main>
      <section class="shell">
        <div class="hero">
          <span class="eyebrow">MCP App Demo</span>
          <h1 id="title">Waiting for tool result</h1>
          <p class="lede" id="subtitle">The playground host should initialize this iframe and stream the tool result in.</p>
        </div>
        <div class="grid">
          <div class="panel">
            <h2>Query</h2>
            <div class="value" id="query">...</div>
            <p class="muted" id="timestamp">No payload yet</p>
          </div>
          <div class="panel">
            <h2>Result JSON</h2>
            <pre id="payload">Awaiting ui/notifications/tool-result</pre>
          </div>
          <div class="panel">
            <h2>Host Bridge</h2>
            <div class="timeline" id="events"></div>
          </div>
        </div>
      </section>
    </main>
    <script>
      const state = {
        toolInput: null,
        toolResult: null,
        hostContext: null,
        events: [],
      };

      const nodes = {
        title: document.getElementById("title"),
        subtitle: document.getElementById("subtitle"),
        query: document.getElementById("query"),
        timestamp: document.getElementById("timestamp"),
        payload: document.getElementById("payload"),
        events: document.getElementById("events"),
      };

      function addEvent(label, detail) {
        state.events.unshift({ label, detail });
        state.events = state.events.slice(0, 4);
        nodes.events.innerHTML = state.events
          .map((entry) => {
            const safeLabel = escapeHtml(entry.label);
            const safeDetail = escapeHtml(entry.detail || "");
            return '<div class="timeline-item"><span class="dot"></span><div><strong>' + safeLabel + '</strong><div class="muted">' + safeDetail + "</div></div></div>";
          })
          .join("");
      }

      function escapeHtml(value) {
        return String(value)
          .replaceAll("&", "&amp;")
          .replaceAll("<", "&lt;")
          .replaceAll(">", "&gt;")
          .replaceAll('"', "&quot;")
          .replaceAll("'", "&#39;");
      }

      function sendRequest(method, params) {
        const id = Math.random().toString(36).slice(2);
        window.parent.postMessage({ jsonrpc: "2.0", id, method, params }, "*");
        return new Promise((resolve, reject) => {
          function onMessage(event) {
            if (event.source !== window.parent) {
              return;
            }
            const message = event.data;
            if (!message || message.id !== id) {
              return;
            }
            window.removeEventListener("message", onMessage);
            if (message.error) {
              reject(new Error(message.error.message || "Request failed"));
              return;
            }
            resolve(message.result);
          }
          window.addEventListener("message", onMessage);
        });
      }

      function parseToolResult(result) {
        const textPart = result?.content?.find((entry) => entry?.type === "text");
        const rawText = textPart?.text;
        if (typeof rawText !== "string") {
          return result;
        }
        try {
          return JSON.parse(rawText);
        } catch {
          return { text: rawText };
        }
      }

      function render() {
        const parsed = parseToolResult(state.toolResult);
        const query = state.toolInput?.arguments?.query || parsed?.query || "No query";
        nodes.query.textContent = query;
        nodes.title.textContent = state.hostContext?.toolInfo?.tool?.name || "Seeded MCP App";
        nodes.subtitle.textContent =
          state.hostContext?.userAgent
            ? "Connected to " + state.hostContext.userAgent
            : "Connected to the playground host";
        nodes.timestamp.textContent = parsed?.generatedAt
          ? "Generated at " + parsed.generatedAt
          : "Awaiting structured payload";
        nodes.payload.textContent = JSON.stringify(
          {
            input: state.toolInput,
            result: state.toolResult,
            parsed,
            hostContext: state.hostContext,
          },
          null,
          2,
        );
      }

      window.addEventListener("message", (event) => {
        if (event.source !== window.parent) {
          return;
        }

        const message = event.data;
        if (!message || typeof message.method !== "string") {
          return;
        }

        if (message.method === "ui/notifications/tool-input") {
          state.toolInput = message.params;
          addEvent("tool-input", "Received tool arguments from host");
          render();
          return;
        }

        if (message.method === "ui/notifications/tool-result") {
          state.toolResult = message.params;
          addEvent("tool-result", "Received tool result from host");
          render();
          return;
        }

        if (message.method === "ui/notifications/host-context-changed") {
          state.hostContext = {
            ...(state.hostContext || {}),
            ...(message.params || {}),
          };
          addEvent("host-context", "Theme: " + (state.hostContext.theme || "unknown"));
          render();
        }
      });

      (async () => {
        addEvent("boot", "Sending ui/initialize");
        const init = await sendRequest("ui/initialize", {
          protocolVersion: "2025-06-18",
          appCapabilities: {
            availableDisplayModes: ["inline"],
          },
          appInfo: {
            name: "gram-seed-app",
            version: "0.0.1",
          },
        });
        state.hostContext = init?.hostContext || null;
        render();
        window.parent.postMessage(
          {
            jsonrpc: "2.0",
            method: "ui/notifications/initialized",
            params: {},
          },
          "*",
        );
        addEvent("initialized", "Waiting for tool data");
      })().catch((error) => {
        nodes.payload.textContent = String(error && error.stack ? error.stack : error);
        addEvent("error", String(error && error.message ? error.message : error));
      });
    </script>
  </body>
</html>`;

  return `
const TOOL_NAME = ${JSON.stringify(PLAYGROUND_MCP_APP_TOOL_NAME)};
const RESOURCE_URI = ${JSON.stringify(PLAYGROUND_MCP_APP_RESOURCE_URI)};
const FUNCTION_SLUG = ${JSON.stringify(functionSlug)};
const HTML = ${JSON.stringify(dashboardHtml)};

export default {
  async handleToolCall({ name, input }) {
    if (name !== TOOL_NAME) {
      return new Response(JSON.stringify({ error: "Unknown tool", name }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      });
    }

    const query =
      typeof input?.query === "string" && input.query.trim().length > 0
        ? input.query.trim()
        : "Gram MCP Apps";

    const payload = {
      slug: FUNCTION_SLUG,
      query,
      generatedAt: new Date().toISOString(),
      cards: [
        "UI metadata comes from the tool + resource definitions",
        "The playground host fetches the UI resource with resources/read",
        "The iframe receives tool-input and tool-result notifications",
      ],
    };

    return new Response(JSON.stringify(payload), {
      headers: { "Content-Type": "application/json" },
    });
  },

  async handleResources({ uri }) {
    if (uri !== RESOURCE_URI) {
      return new Response("Unknown resource", {
        status: 404,
        headers: { "Content-Type": "text/plain" },
      });
    }

    return new Response(HTML, {
      headers: { "Content-Type": "text/html;profile=mcp-app" },
    });
  },
};
`;
}

type Toolset = {
  created: boolean;
  slug: string;
  mcpURL: string;
  toolUrns: string[];
};

async function upsertToolset(init: {
  gram: GramCore;
  serverURL: string;
  sessionId: string;
  projectSlug: string;
  deploymentId: string;
  asset: Asset;
  mcpPublic: boolean;
}): Promise<Toolset> {
  const {
    gram,
    serverURL,
    sessionId,
    projectSlug,
    deploymentId,
    asset,
    mcpPublic,
  } = init;

  const urnPrefix =
    asset.type === "functions"
      ? `tools:function:${asset.slug}`
      : `tools:http:${asset.slug}`;

  // Fetch tools filtered by URN prefix
  const toolRes = await toolsList(
    gram,
    { urnPrefix },
    {
      projectSlugHeaderGramProject: projectSlug,
      sessionHeaderGramSession: sessionId,
    },
  );
  if (!toolRes.ok) {
    abort(`Failed to list tools for project \`${projectSlug}\``, toolRes.error);
  }
  const toolUrns = toolRes.value.tools.map((t) => {
    switch (true) {
      case !!t.httpToolDefinition:
        return t.httpToolDefinition.toolUrn;
      case !!t.functionToolDefinition:
        return t.functionToolDefinition.toolUrn;
      case !!t.externalMcpToolDefinition:
        return t.externalMcpToolDefinition.toolUrn;
      case !!t.promptTemplate:
        return t.promptTemplate.toolUrn;
      default:
        assert(false, "Unknown tool type: " + JSON.stringify(t));
    }
  });

  let resourceUrns: string[] | undefined;
  if (asset.type === "functions" && asset.resourceUris.length > 0) {
    const resourceRes = await resourcesList(
      gram,
      {
        deploymentId,
      },
      {
        projectSlugHeaderGramProject: projectSlug,
        sessionHeaderGramSession: sessionId,
      },
    );
    if (!resourceRes.ok) {
      abort(
        `Failed to list resources for project \`${projectSlug}\``,
        resourceRes.error,
      );
    }

    const wantedUris = new Set(asset.resourceUris);
    resourceUrns = resourceRes.value.resources
      .map((resource) => resource.functionResourceDefinition)
      .filter((resource) => resource !== undefined)
      .filter((resource) => wantedUris.has(resource.uri))
      .map((resource) => resource.resourceUrn);

    if (resourceUrns.length !== wantedUris.size) {
      abort(
        `Failed to resolve seeded MCP app resources for asset \`${asset.slug}\``,
        resourceRes.value.resources,
      );
    }
  }

  let toolset: Toolset;
  const name = asset.slug + "-seed";

  const createRes = await toolsetsCreate(
    gram,
    {
      createToolsetRequestBody: {
        name,
        resourceUrns,
        toolUrns,
      },
    },
    {
      option1: {
        projectSlugHeaderGramProject: projectSlug,
        sessionHeaderGramSession: sessionId,
      },
    },
  );
  switch (true) {
    case !createRes.ok &&
      createRes.error instanceof ServiceError &&
      createRes.error.data$.name === "conflict":
      const updateRes = await toolsetsUpdateBySlug(
        gram,
        {
          slug: name,
          updateToolsetRequestBody: {
            resourceUrns,
            toolUrns,
          },
        },
        {
          option1: {
            projectSlugHeaderGramProject: projectSlug,
            sessionHeaderGramSession: sessionId,
          },
        },
      );
      if (!updateRes.ok) {
        abort(
          `Failed to update toolset '${name}' for project '${projectSlug}'`,
          updateRes.error,
        );
      }
      toolset = {
        created: false,
        slug: updateRes.value.slug,
        mcpURL: `${serverURL}/mcp/${updateRes.value.mcpSlug}`,
        toolUrns,
      };
      break;
    case !createRes.ok:
      abort(
        `Failed to create toolset '${name}' for project '${projectSlug}'`,
        createRes.error,
      );
    default:
      toolset = {
        created: true,
        slug: createRes.value.slug,
        mcpURL: `${serverURL}/mcp/${createRes.value.mcpSlug}`,
        toolUrns,
      };
      break;
  }

  if (!mcpPublic) {
    return toolset;
  }

  const updateRes = await toolsetsUpdateBySlug(
    gram,
    {
      slug: toolset.slug,
      updateToolsetRequestBody: {
        mcpIsPublic: true,
        mcpEnabled: true,
      },
    },
    {
      option1: {
        sessionHeaderGramSession: sessionId,
        projectSlugHeaderGramProject: projectSlug,
      },
    },
  );
  if (!updateRes.ok) {
    abort(
      `Failed to make toolset '${toolset.slug}' public for project '${projectSlug}'`,
      updateRes.error,
    );
  }

  toolset.mcpURL = `${serverURL}/mcp/${updateRes.value.mcpSlug}`;

  log.info(`${toolset.mcpURL} visibility was changed to public`);

  return toolset;
}

// The Gram API tools that compose the built-in MCP Logs server.
// These match the production `speakeasy-team-mcp-logs` toolset.
const MCP_LOGS_TOOL_URNS = new Set([
  "tools:http:gram:gram_list_tools",
  "tools:http:gram:gram_search_logs",
  "tools:http:gram:gram_list_global_variations",
  "tools:http:gram:gram_search_tool_calls",
  "tools:http:gram:gram_get_toolset",
  "tools:http:gram:gram_get_observability_overview",
  "tools:http:gram:gram_list_toolsets",
  "tools:http:gram:gram_get_mcp_metadata",
  "tools:http:gram:gram_list_chats_with_resolutions",
  "tools:http:gram:gram_get_deployment_logs",
  "tools:http:gram:gram_list_chats",
  // Audit log tools
  "tools:http:gram:gram_list_audit_logs",
  "tools:http:gram:gram_list_audit_log_facets",
  // Employee directory tools
  "tools:http:gram:gram_list_organization_users",
]);

async function upsertMcpLogsToolset(init: {
  gram: GramCore;
  serverURL: string;
  sessionId: string;
  projectSlug: string;
}): Promise<Toolset> {
  const { gram, serverURL, sessionId, projectSlug } = init;

  // List all tools from the `gram` asset, then filter to the MCP Logs subset
  const toolRes = await toolsList(
    gram,
    { urnPrefix: "tools:http:gram" },
    {
      projectSlugHeaderGramProject: projectSlug,
      sessionHeaderGramSession: sessionId,
    },
  );
  if (!toolRes.ok) {
    abort(
      `Failed to list tools for MCP Logs toolset in project '${projectSlug}'`,
      toolRes.error,
    );
  }

  const toolUrns = toolRes.value.tools
    .map((t) => {
      switch (true) {
        case !!t.httpToolDefinition:
          return t.httpToolDefinition.toolUrn;
        case !!t.functionToolDefinition:
          return t.functionToolDefinition.toolUrn;
        case !!t.externalMcpToolDefinition:
          return t.externalMcpToolDefinition.toolUrn;
        case !!t.promptTemplate:
          return t.promptTemplate.toolUrn;
        default:
          assert(false, "Unknown tool type: " + JSON.stringify(t));
      }
    })
    .filter((urn) => MCP_LOGS_TOOL_URNS.has(urn));

  const name = "mcp-logs";

  const createRes = await toolsetsCreate(
    gram,
    {
      createToolsetRequestBody: {
        name,
        toolUrns,
      },
    },
    {
      option1: {
        projectSlugHeaderGramProject: projectSlug,
        sessionHeaderGramSession: sessionId,
      },
    },
  );

  let toolset: Toolset;
  switch (true) {
    case !createRes.ok &&
      createRes.error instanceof ServiceError &&
      createRes.error.data$.name === "conflict":
      const updateRes = await toolsetsUpdateBySlug(
        gram,
        {
          slug: name,
          updateToolsetRequestBody: {
            toolUrns,
            mcpIsPublic: true,
            mcpEnabled: true,
          },
        },
        {
          option1: {
            projectSlugHeaderGramProject: projectSlug,
            sessionHeaderGramSession: sessionId,
          },
        },
      );
      if (!updateRes.ok) {
        abort(
          `Failed to update MCP Logs toolset for project '${projectSlug}'`,
          updateRes.error,
        );
      }
      toolset = {
        created: false,
        slug: updateRes.value.slug,
        mcpURL: `${serverURL}/mcp/${updateRes.value.mcpSlug}`,
        toolUrns: updateRes.value.toolUrns,
      };
      break;
    case !createRes.ok:
      abort(
        `Failed to create MCP Logs toolset for project '${projectSlug}'`,
        createRes.error,
      );
    default:
      // Make it public + MCP enabled right after creation
      const publicRes = await toolsetsUpdateBySlug(
        gram,
        {
          slug: createRes.value.slug,
          updateToolsetRequestBody: {
            mcpIsPublic: true,
            mcpEnabled: true,
          },
        },
        {
          option1: {
            projectSlugHeaderGramProject: projectSlug,
            sessionHeaderGramSession: sessionId,
          },
        },
      );
      if (!publicRes.ok) {
        abort(
          `Failed to make MCP Logs toolset public for project '${projectSlug}'`,
          publicRes.error,
        );
      }
      toolset = {
        created: true,
        slug: publicRes.value.slug,
        mcpURL: `${serverURL}/mcp/${publicRes.value.mcpSlug}`,
        toolUrns: createRes.value.toolUrns,
      };
      break;
  }

  return toolset;
}

// Namespace UUID for generating deterministic chat IDs
const CHAT_UUID_NAMESPACE = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"; // DNS namespace

function generateChatUUID(chatNumber: number): string {
  // Generate a deterministic UUID v5 from the chat number
  const hash = crypto
    .createHash("sha1")
    .update(CHAT_UUID_NAMESPACE)
    .update(`chat-${chatNumber}`)
    .digest();

  // Set version (5) and variant bits
  hash[6] = (hash[6] & 0x0f) | 0x50;
  hash[8] = (hash[8] & 0x3f) | 0x80;

  const hex = hash.toString("hex").slice(0, 32);
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`;
}

async function seedObservabilityData(init: {
  projectId: string;
  organizationId: string;
  toolUrns: string[];
}): Promise<void> {
  const { projectId, organizationId, toolUrns } = init;

  log.info(`Seeding observability data with ${toolUrns.length} tool URNs...`);

  if (toolUrns.length === 0) {
    log.warn(
      "No tool URNs available for seeding observability data. Skipping.",
    );
    return;
  }

  const NUM_CHATS = 500; // Reduced for faster seeding
  const DAYS_BACK = 30;
  const NUM_HOOKS = 6000;

  // Use actual tool URNs from the deployment
  const TOOLS = toolUrns;

  const RESOLUTIONS = ["success", "partial", "failure"] as const;
  const RESOLUTION_WEIGHTS = [65, 15, 20]; // success: 65%, partial: 15%, failure: 20%

  // Sample user messages for chat content
  const USER_MESSAGES = [
    "Can you help me list all my GitHub repositories?",
    "Send a message to the #general channel on Slack",
    "Query the database for recent orders",
    "Generate a summary of this document",
    "Create a new Jira ticket for this bug",
    "Process a payment for this order",
    "Create a new page in Notion with these notes",
    "What's the status of my last deployment?",
    "Help me debug this API integration",
    "Summarize the customer feedback from last week",
  ];

  const ASSISTANT_RESPONSES = [
    "I'll help you with that. Let me check...",
    "Sure, I'm processing your request now.",
    "I've completed the task. Here are the results:",
    "I found the following information for you:",
    "The operation was successful. Here's what happened:",
  ];

  const SYSTEM_PROMPTS = [
    "You are a helpful AI assistant with access to various tools. You can help users manage their GitHub repositories, send Slack messages, query databases, and more. Always be concise and helpful.",
    "You are an enterprise assistant for Acme Corp. You have access to internal tools and databases. Follow company policies and maintain confidentiality. Be professional and efficient.",
    "You are a technical support agent. Help users troubleshoot issues with their integrations. Ask clarifying questions when needed. Provide step-by-step instructions.",
    "You are a data analyst assistant. You can query databases, generate reports, and visualize data. Always explain your methodology and assumptions.",
    "You are a project management assistant. Help users track tasks, manage deadlines, and coordinate with team members. Keep responses organized and actionable.",
  ];

  // Helper to generate large arrays for testing truncation
  function generateLargeRepositoryList(count: number) {
    const languages = [
      "Go",
      "TypeScript",
      "Python",
      "Rust",
      "Java",
      "Ruby",
      "C++",
      "JavaScript",
    ];
    const repos = [];
    for (let i = 0; i < count; i++) {
      repos.push({
        id: `repo-${i + 1}`,
        name: `project-${["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"][i % 8]}-${Math.floor(i / 8) + 1}`,
        full_name: `acme-corp/project-${i + 1}`,
        description: `This is a comprehensive ${languages[i % languages.length]} project for handling ${["data processing", "API management", "user authentication", "payment processing", "analytics", "monitoring", "notifications", "scheduling"][i % 8]}`,
        language: languages[i % languages.length],
        stars: Math.floor(Math.random() * 1000),
        forks: Math.floor(Math.random() * 200),
        open_issues: Math.floor(Math.random() * 50),
        watchers: Math.floor(Math.random() * 500),
        default_branch: "main",
        visibility: i % 3 === 0 ? "public" : "private",
        created_at: new Date(
          Date.now() - Math.random() * 365 * 24 * 60 * 60 * 1000,
        ).toISOString(),
        updated_at: new Date(
          Date.now() - Math.random() * 30 * 24 * 60 * 60 * 1000,
        ).toISOString(),
        pushed_at: new Date(
          Date.now() - Math.random() * 7 * 24 * 60 * 60 * 1000,
        ).toISOString(),
        topics: [
          "backend",
          "api",
          languages[i % languages.length].toLowerCase(),
        ],
        license: { key: "mit", name: "MIT License" },
        permissions: { admin: true, push: true, pull: true },
      });
    }
    return repos;
  }

  function generateLargeLogEntries(count: number) {
    const levels = ["DEBUG", "INFO", "WARN", "ERROR"];
    const services = [
      "api-gateway",
      "auth-service",
      "payment-processor",
      "notification-worker",
      "analytics-pipeline",
    ];
    const messages = [
      "Request received from client",
      "Processing authentication token",
      "Database query executed successfully",
      "Cache miss - fetching from primary store",
      "Rate limit check passed",
      "Request payload validated",
      "Initiating downstream API call",
      "Response serialization complete",
      "Metric recorded for monitoring",
      "Audit log entry created",
      "Connection pool status: healthy",
      "Memory usage within threshold",
      "CPU utilization normal",
      "Garbage collection completed",
      "Health check endpoint accessed",
    ];
    const logs = [];
    const baseTime = Date.now();
    for (let i = 0; i < count; i++) {
      logs.push({
        timestamp: new Date(baseTime - (count - i) * 100).toISOString(),
        level: levels[Math.floor(Math.random() * levels.length)],
        service: services[Math.floor(Math.random() * services.length)],
        message: messages[Math.floor(Math.random() * messages.length)],
        trace_id: `trace-${Math.random().toString(36).substring(2, 15)}`,
        span_id: `span-${Math.random().toString(36).substring(2, 10)}`,
        request_id: `req-${Math.random().toString(36).substring(2, 12)}`,
        user_id: `user-${Math.floor(Math.random() * 1000)}`,
        duration_ms: Math.floor(Math.random() * 500),
        metadata: {
          host: `server-${Math.floor(Math.random() * 10) + 1}.prod.example.com`,
          region: ["us-east-1", "us-west-2", "eu-west-1"][
            Math.floor(Math.random() * 3)
          ],
          version: `v${Math.floor(Math.random() * 3) + 1}.${Math.floor(Math.random() * 10)}.${Math.floor(Math.random() * 20)}`,
        },
      });
    }
    return logs;
  }

  function generateLargeOrderList(count: number) {
    const statuses = [
      "pending",
      "processing",
      "shipped",
      "delivered",
      "cancelled",
    ];
    const customers = [
      "Acme Corp",
      "TechStart Inc",
      "Global Services",
      "DataFlow LLC",
      "CloudNine Solutions",
      "NextGen Systems",
      "Pioneer Tech",
      "Quantum Labs",
    ];
    const orders = [];
    for (let i = 0; i < count; i++) {
      const itemCount = Math.floor(Math.random() * 5) + 1;
      const items = [];
      for (let j = 0; j < itemCount; j++) {
        items.push({
          sku: `SKU-${Math.random().toString(36).substring(2, 8).toUpperCase()}`,
          name: `Product ${j + 1}`,
          quantity: Math.floor(Math.random() * 10) + 1,
          unit_price: Math.floor(Math.random() * 500) + 10,
          discount: Math.random() > 0.7 ? Math.floor(Math.random() * 20) : 0,
        });
      }
      orders.push({
        id: `ORD-${String(i + 1).padStart(6, "0")}`,
        customer: {
          name: customers[Math.floor(Math.random() * customers.length)],
          email: `contact@${customers[Math.floor(Math.random() * customers.length)].toLowerCase().replace(/\s+/g, "")}.com`,
          phone: `+1-555-${String(Math.floor(Math.random() * 10000)).padStart(4, "0")}`,
        },
        items,
        subtotal: items.reduce(
          (sum, item) => sum + item.quantity * item.unit_price,
          0,
        ),
        tax: Math.floor(Math.random() * 100),
        shipping: Math.floor(Math.random() * 50),
        total: 0,
        status: statuses[Math.floor(Math.random() * statuses.length)],
        shipping_address: {
          street: `${Math.floor(Math.random() * 9999) + 1} Main St`,
          city: ["New York", "San Francisco", "Chicago", "Austin", "Seattle"][
            Math.floor(Math.random() * 5)
          ],
          state: ["NY", "CA", "IL", "TX", "WA"][Math.floor(Math.random() * 5)],
          zip: String(Math.floor(Math.random() * 90000) + 10000),
          country: "US",
        },
        created_at: new Date(
          Date.now() - Math.random() * 30 * 24 * 60 * 60 * 1000,
        ).toISOString(),
        updated_at: new Date(
          Date.now() - Math.random() * 7 * 24 * 60 * 60 * 1000,
        ).toISOString(),
      });
    }
    return orders;
  }

  // Sample tool call outputs with realistic JSON content
  // Includes both short and very long outputs to test truncation
  const TOOL_OUTPUTS = [
    // SHORT OUTPUT - simple response
    {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            message_sent: true,
            channel: "#general",
            timestamp: "1705312200.000100",
            message_id: "msg_abc123def456",
          }),
        },
      ],
    },
    // LONG OUTPUT - 30 repositories (~600+ lines when formatted)
    {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            repositories: generateLargeRepositoryList(30),
            total_count: 30,
            pagination: { page: 1, per_page: 30, total_pages: 1 },
          }),
        },
      ],
    },
    // LONG OUTPUT - 80 log entries (~1000+ lines when formatted)
    {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            logs: generateLargeLogEntries(80),
            total_entries: 80,
            query: {
              start_time: "2024-01-15T00:00:00Z",
              end_time: "2024-01-15T23:59:59Z",
              level: "all",
            },
          }),
        },
      ],
    },
    // LONG OUTPUT - 20 orders with items (~600+ lines when formatted)
    {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            orders: generateLargeOrderList(20),
            pagination: { page: 1, per_page: 20, total: 156 },
            summary: { total_revenue: 125000, average_order_value: 2500 },
          }),
        },
      ],
    },
    // MEDIUM OUTPUT - deployment with detailed logs
    {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            deployment: {
              id: "deploy-789xyz",
              status: "success",
              environment: "production",
              started_at: "2024-01-15T08:00:00Z",
              completed_at: "2024-01-15T08:05:32Z",
              commit_sha: "a1b2c3d4e5f6",
              logs_url: "https://logs.example.com/deploy-789xyz",
              stages: [
                {
                  name: "build",
                  status: "success",
                  duration_seconds: 45,
                  logs: generateLargeLogEntries(10),
                },
                {
                  name: "test",
                  status: "success",
                  duration_seconds: 120,
                  logs: generateLargeLogEntries(15),
                },
                {
                  name: "deploy",
                  status: "success",
                  duration_seconds: 30,
                  logs: generateLargeLogEntries(8),
                },
              ],
            },
          }),
        },
      ],
    },
    // SHORT OUTPUT - ticket created
    {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            ticket_created: true,
            ticket_id: "JIRA-4521",
            project: "BACKEND",
            summary: "API returns 500 on large payloads",
            priority: "High",
            assignee: "john.doe@example.com",
            url: "https://jira.example.com/browse/JIRA-4521",
          }),
        },
      ],
    },
  ];

  // Generate chat data
  const now = Date.now();
  const msPerDay = 24 * 60 * 60 * 1000;

  // Build PostgreSQL insert statements
  let chatsSQL = `
    DELETE FROM chats WHERE project_id = '${projectId}';
    INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id, title, created_at, updated_at) VALUES
  `;

  let messagesSQL = `
    INSERT INTO chat_messages (chat_id, project_id, role, content, model, created_at) VALUES
  `;

  let resolutionsSQL = `
    INSERT INTO chat_resolutions (project_id, chat_id, user_goal, resolution, resolution_notes, score, created_at) VALUES
  `;

  const chatValues: string[] = [];
  const messageValues: string[] = [];
  const resolutionValues: string[] = [];

  for (let i = 0; i < NUM_CHATS; i++) {
    const chatId = generateChatUUID(i);
    const extUserId = `ext-user-${i % 80}`;
    const userId = `user-${i % 200}`;

    // Random time within the past DAYS_BACK days
    const daysAgo = Math.random() * DAYS_BACK;
    const chatTime = new Date(now - daysAgo * msPerDay);
    const updatedTime = new Date(
      chatTime.getTime() + Math.random() * 10 * 60 * 1000,
    ); // 0-10 minutes later

    // Generate a title from the first user message
    const userMsg =
      USER_MESSAGES[Math.floor(Math.random() * USER_MESSAGES.length)];
    const title = userMsg.slice(0, 50) + (userMsg.length > 50 ? "..." : "");

    chatValues.push(
      `('${chatId}', '${projectId}', '${organizationId}', '${userId}', '${extUserId}', '${title.replace(/'/g, "''")}', '${chatTime.toISOString()}', '${updatedTime.toISOString()}')`,
    );

    // Generate 2-6 messages per chat
    const numMessages = 2 + Math.floor(Math.random() * 5);
    let msgTime = chatTime;

    // Add system message at the start (80% of chats have system prompts)
    if (Math.random() < 0.8) {
      const systemPrompt =
        SYSTEM_PROMPTS[Math.floor(Math.random() * SYSTEM_PROMPTS.length)];
      messageValues.push(
        `('${chatId}', '${projectId}', 'system', '${systemPrompt.replace(/'/g, "''")}', 'gpt-4', '${msgTime.toISOString()}')`,
      );
      msgTime = new Date(msgTime.getTime() + 100); // Tiny increment for system message
    }

    for (let j = 0; j < numMessages; j++) {
      const role = j % 2 === 0 ? "user" : "assistant";
      const content =
        role === "user"
          ? USER_MESSAGES[Math.floor(Math.random() * USER_MESSAGES.length)]
          : ASSISTANT_RESPONSES[
              Math.floor(Math.random() * ASSISTANT_RESPONSES.length)
            ];

      msgTime = new Date(msgTime.getTime() + Math.random() * 30 * 1000); // 0-30 seconds later

      messageValues.push(
        `('${chatId}', '${projectId}', '${role}', '${content.replace(/'/g, "''")}', 'gpt-4', '${msgTime.toISOString()}')`,
      );

      // After assistant messages, 60% chance to add a tool call result
      if (role === "assistant" && Math.random() < 0.6) {
        const toolOutput =
          TOOL_OUTPUTS[Math.floor(Math.random() * TOOL_OUTPUTS.length)];
        const toolContent = JSON.stringify(toolOutput).replace(/'/g, "''");
        msgTime = new Date(msgTime.getTime() + Math.random() * 5 * 1000); // 0-5 seconds later

        messageValues.push(
          `('${chatId}', '${projectId}', 'tool', '${toolContent}', 'gpt-4', '${msgTime.toISOString()}')`,
        );
      }
    }

    // Generate resolution (70% of chats have resolutions)
    if (Math.random() < 0.7) {
      const rand = Math.random() * 100;
      let resolution: (typeof RESOLUTIONS)[number];
      let score: number;

      if (rand < RESOLUTION_WEIGHTS[0]) {
        resolution = "success";
        score = 80 + Math.floor(Math.random() * 21); // 80-100
      } else if (rand < RESOLUTION_WEIGHTS[0] + RESOLUTION_WEIGHTS[1]) {
        resolution = "partial";
        score = 40 + Math.floor(Math.random() * 31); // 40-70
      } else {
        resolution = "failure";
        score = Math.floor(Math.random() * 30); // 0-29
      }

      const resolutionNotes =
        resolution === "success"
          ? "User goal was fully achieved"
          : resolution === "partial"
            ? "User goal was partially achieved"
            : "User goal could not be completed";

      resolutionValues.push(
        `('${projectId}', '${chatId}', '${userMsg.replace(/'/g, "''")}', '${resolution}', '${resolutionNotes}', ${score}, '${updatedTime.toISOString()}')`,
      );
    }
  }

  chatsSQL += chatValues.join(",\n") + ";";
  messagesSQL += messageValues.join(",\n") + ";";

  if (resolutionValues.length > 0) {
    resolutionsSQL += resolutionValues.join(",\n") + ";";
  }

  // Execute PostgreSQL inserts
  const pgSQL = `
    BEGIN;
    ${chatsSQL}
    ${messagesSQL}
    ${resolutionValues.length > 0 ? resolutionsSQL : ""}
    COMMIT;
  `;

  try {
    // Use individual env vars to avoid search_path issue with psql
    const dbUser = process.env.DB_USER || "gram";
    const dbName = process.env.DB_NAME || "gram";

    // Write SQL to temp file to avoid E2BIG (arg list too long) error
    const tmpFile = path.join(process.cwd(), ".seed-observability.sql");
    await fs.writeFile(tmpFile, pgSQL, "utf-8");

    try {
      await $`docker compose cp ${tmpFile} gram-db:/tmp/seed.sql`.quiet();
      await $`docker compose exec gram-db psql -U ${dbUser} -d ${dbName} -f /tmp/seed.sql`.quiet();
      log.info(`Inserted ${NUM_CHATS} chats with messages into PostgreSQL`);
    } finally {
      // Clean up temp file
      await fs.unlink(tmpFile).catch(() => {});
    }
  } catch (e: unknown) {
    const err = e as { stderr?: string; stdout?: string; message?: string };
    log.warn(
      `Failed to seed PostgreSQL: ${err.message || err.stderr || err.stdout || JSON.stringify(e)}`,
    );
  }

  // Build ClickHouse insert for telemetry logs
  // We'll create a simpler inline insert using clickhouse-client
  const chInserts: string[] = [];

  for (let i = 0; i < NUM_CHATS; i++) {
    const chatId = generateChatUUID(i);
    const extUserId = `ext-user-${i % 80}`;
    const userId = `user-${i % 200}`;
    const apiKeyId = `key-${i % 5}`;

    const daysAgo = Math.random() * DAYS_BACK;
    const eventTime = new Date(now - daysAgo * msPerDay);
    const timeNano = BigInt(eventTime.getTime()) * BigInt(1000000);

    // Generate a unique trace ID for each tool call (32 hex chars)
    const traceId = crypto.randomBytes(16).toString("hex");

    // Tool call event - TOOLS now contains full URNs like "tools:http:gram:operation"
    const toolUrn = TOOLS[Math.floor(Math.random() * TOOLS.length)];
    const statusCode =
      Math.random() < 0.92
        ? 200
        : [400, 500, 502][Math.floor(Math.random() * 3)];
    const latency = (0.05 + Math.random() * 2).toFixed(3);

    chInserts.push(
      `(${timeNano}, ${timeNano}, 'INFO', 'Tool call: ${toolUrn}', '${traceId}', '{"http.response.status_code": ${statusCode}, "http.server.request.duration": ${latency}, "gram.tool.urn": "${toolUrn}", "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}"}', '{"gram.deployment.id": "deployment-1"}', '${projectId}', '${toolUrn}', 'gram-mcp-gateway', '${chatId}')`,
    );

    // Chat completion event - same trace ID links it to the tool call
    const finishReason =
      Math.random() < 0.65 ? "stop" : Math.random() < 0.9 ? "length" : "error";
    const duration = 30 + Math.floor(Math.random() * 150);
    const completionStatus = Math.random() < 0.92 ? 200 : 500;

    chInserts.push(
      `(${timeNano + BigInt(1000000)}, ${timeNano + BigInt(1000000)}, 'INFO', 'Chat completion', '${traceId}', '{"gen_ai.response.finish_reasons": ["${finishReason}"], "gen_ai.conversation.id": "${chatId}", "gen_ai.conversation.duration": ${duration}, "gram.resource.urn": "agents:chat:completion", "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}", "http.response.status_code": ${completionStatus}}', '{"gram.deployment.id": "deployment-1"}', '${projectId}', 'agents:chat:completion', 'gram-mcp-gateway', '${chatId}')`,
    );

    // Resolution event (70% of chats) - same trace ID
    if (Math.random() < 0.7) {
      const rand = Math.random() * 100;
      let resolution: string;
      let score: number;

      if (rand < 65) {
        resolution = "success";
        score = 80 + Math.floor(Math.random() * 21);
      } else if (rand < 80) {
        resolution = "partial";
        score = 40 + Math.floor(Math.random() * 31);
      } else {
        resolution = "failure";
        score = Math.floor(Math.random() * 30);
      }

      chInserts.push(
        `(${timeNano + BigInt(2000000)}, ${timeNano + BigInt(2000000)}, 'INFO', 'Chat resolution: ${resolution}', '${traceId}', '{"gen_ai.evaluation.name": "chat_resolution", "gen_ai.evaluation.score.label": "${resolution}", "gen_ai.evaluation.score.value": ${score}, "gen_ai.conversation.id": "${chatId}", "gen_ai.conversation.duration": ${duration}, "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}"}', '{"gram.deployment.id": "deployment-1"}', '${projectId}', 'chat_resolution', 'gram-resolution-analyzer', '${chatId}')`,
      );
    }
  }

  // Hook-specific constants
  const HOOK_SOURCES = ["claude", "vscode", "cli", "api"];
  const HOOK_SOURCE_WEIGHTS = [45, 39, 11, 6];
  const HOOK_SOURCE_TOTAL = HOOK_SOURCE_WEIGHTS.reduce((s, w) => s + w, 0);

  const MCP_SERVER_CONFIGS = [
    {
      name: "slack",
      tools: [
        "slack_search_public_and_private",
        "slack_send_message",
        "slack_read_channel",
        "slack_read_thread",
        "slack_search_channels",
        "slack_search_public",
      ],
      weight: 30,
      failureRate: 0.07,
    },
    {
      name: "notion",
      tools: [
        "notion-search",
        "notion-fetch",
        "notion-create-pages",
        "notion-get-users",
        "notion-update-page",
        "notion-create-comment",
      ],
      weight: 14,
      failureRate: 0.02,
    },
    {
      name: "claude_ai_Slack",
      tools: [
        "slack_read_channel",
        "slack_send_message",
        "slack_search_channels",
        "slack_read_thread",
        "slack_search_users",
      ],
      weight: 9,
      failureRate: 0.04,
    },
    {
      name: "datadog-mcp",
      tools: [
        "get_datadog_metric",
        "analyze_datadog_logs",
        "aggregate_spans",
        "list_dashboards",
        "query_logs",
        "get_trace",
      ],
      weight: 7,
      failureRate: 0.06,
    },
    {
      name: "linear",
      tools: [
        "list_issues",
        "get_issue",
        "save_issue",
        "list_teams",
        "save_comment",
      ],
      weight: 5,
      failureRate: 0.06,
    },
    {
      name: "Claude_in_Chrome",
      tools: ["computer", "navigate", "screenshot", "click", "type"],
      weight: 3,
      failureRate: 0.06,
    },
    {
      name: "local",
      tools: [
        "Skill",
        "Write",
        "Read",
        "Edit",
        "Grep",
        "ToolSearch",
        "Web Search",
        "Bash",
      ],
      weight: 8,
      failureRate: 0.05,
    },
    {
      name: "claude_ai_Linear",
      tools: [
        "get_issue",
        "list_comments",
        "save_issue",
        "list_issues",
        "save_comment",
        "get_team",
        "list_teams",
      ],
      weight: 3,
      failureRate: 0.09,
    },
    {
      name: "plugin_slack_slack",
      tools: [
        "slack_read_channel",
        "slack_search_public_and_private",
        "slack_send_message",
        "slack_read_thread",
      ],
      weight: 2,
      failureRate: 0.64,
    },
    {
      name: "claude_ai_HubSpot",
      tools: ["search_crm_objects", "get_contact", "list_contacts"],
      weight: 2,
      failureRate: 0.02,
    },
    {
      name: "claude_ai_Figma",
      tools: [
        "get_design_context",
        "get_screenshot",
        "get_metadata",
        "get_figjam",
        "search_design_system",
      ],
      weight: 2,
      failureRate: 0.35,
    },
    {
      name: "Gmail",
      tools: [
        "search_threads",
        "get_thread",
        "create_draft",
        "list_labels",
        "send_message",
      ],
      weight: 2,
      failureRate: 0.0,
    },
    {
      name: "claude_ai_Datadog",
      tools: [
        "get_datadog_metric",
        "analyze_datadog_logs",
        "list_monitors",
        "list_dashboards",
      ],
      weight: 1,
      failureRate: 0.1,
    },
  ];
  const SERVER_TOTAL_WEIGHT = MCP_SERVER_CONFIGS.reduce(
    (s, c) => s + c.weight,
    0,
  );

  const USER_EMAILS = [
    "thomas@example.com",
    "daniel@example.com",
    "adam@example.com",
    "katrina@example.com",
    "brian@example.com",
    "sagar@example.com",
    "quinn@example.com",
    "chase@example.com",
    "subomi@example.com",
    "alex@example.com",
    "tiago@example.com",
  ];
  const USER_EMAIL_WEIGHTS = [25, 20, 18, 12, 10, 8, 5, 2, 3, 2, 1];
  const USER_EMAIL_TOTAL = USER_EMAIL_WEIGHTS.reduce((s, w) => s + w, 0);

  const SKILL_NAMES = [
    "datadog",
    "frontend",
    "golang",
    "postgresql",
    "clickhouse",
    "pr",
    "standup",
    "mise-tasks",
    "pdf",
    "caveman",
    "code-review",
    "generate-tests",
    "write-commit-msg",
    "explain-error",
    "summarize-pr",
    "draft-docs",
    "debug-issue",
  ];
  const SKILL_NAME_WEIGHTS = [
    18, 16, 14, 12, 10, 9, 8, 6, 5, 4, 3, 3, 3, 2, 2, 2, 2,
  ];
  const SKILL_NAME_TOTAL = SKILL_NAME_WEIGHTS.reduce((s, w) => s + w, 0);

  function sqlAttrs(attrs: Record<string, any>): string {
    return JSON.stringify(attrs).replace(/\\/g, "\\\\").replace(/'/g, "\\'");
  }

  function weightedPick<T>(items: T[], weights: number[], total: number): T {
    const roll = Math.random() * total;
    let accum = 0;
    for (let j = 0; j < items.length; j++) {
      accum += weights[j];
      if (roll < accum) return items[j];
    }
    return items[items.length - 1];
  }

  for (let i = 0; i < NUM_HOOKS; i++) {
    const daysAgo = Math.random() * DAYS_BACK;
    const sessionId = `session-${Math.floor(daysAgo * 24)}`; // one session per hour

    const serverRoll = Math.random() * SERVER_TOTAL_WEIGHT;
    let serverAccum = 0;
    let serverConfig = MCP_SERVER_CONFIGS[0];
    for (const cfg of MCP_SERVER_CONFIGS) {
      serverAccum += cfg.weight;
      if (serverRoll < serverAccum) {
        serverConfig = cfg;
        break;
      }
    }
    const toolName =
      serverConfig.tools[Math.floor(Math.random() * serverConfig.tools.length)];
    const mcpServer = serverConfig.name;

    const toolUseId = `toolu_${crypto.randomBytes(12).toString("hex")}`;
    const hookSource = weightedPick(
      HOOK_SOURCES,
      HOOK_SOURCE_WEIGHTS,
      HOOK_SOURCE_TOTAL,
    );
    const userEmail = weightedPick(
      USER_EMAILS,
      USER_EMAIL_WEIGHTS,
      USER_EMAIL_TOTAL,
    );

    const eventTime = new Date(now - daysAgo * msPerDay);
    const baseTimeNano = BigInt(eventTime.getTime()) * BigInt(1000000);

    // Generate a unique trace ID for this tool call (32 hex chars)
    const traceId = crypto.randomBytes(16).toString("hex");

    // Decide if this is a successful call or failure
    const isFailure = Math.random() < serverConfig.failureRate;

    // 1. SessionStart event (10% of the time)
    if (Math.random() < 0.1) {
      const attrs: Record<string, any> = {
        "gram.event.source": "hook",
        "gram.hook.event": "SessionStart",
        "gram.hook.source": hookSource,
        "gram.project.id": projectId,
        "gen_ai.conversation.id": sessionId,
      };
      if (userEmail) attrs["user.email"] = userEmail;

      chInserts.push(
        `(${baseTimeNano}, ${baseTimeNano}, 'INFO', 'Hook: SessionStart', '${traceId}', '${sqlAttrs(attrs)}', '{}', '${projectId}', 'SessionStart', '${hookSource}', '${sessionId}')`,
      );
    } else {
      const skillName =
        toolName === "Skill"
          ? weightedPick(SKILL_NAMES, SKILL_NAME_WEIGHTS, SKILL_NAME_TOTAL)
          : null;

      // 2. PreToolUse event
      const preToolAttrs: Record<string, any> = {
        "gram.event.source": "hook",
        "gram.tool.name": toolName,
        "gram.hook.event": "PreToolUse",
        "gram.hook.source": hookSource,
        "gram.project.id": projectId,
        "gen_ai.conversation.id": sessionId,
        "gen_ai.tool_call.id": toolUseId,
      };
      if (userEmail) preToolAttrs["user.email"] = userEmail;
      if (mcpServer && toolName !== "Skill")
        preToolAttrs["gram.tool_call.source"] = mcpServer;
      if (skillName)
        preToolAttrs["gen_ai.tool.call.arguments"] = JSON.stringify({
          skill: skillName,
        });

      chInserts.push(
        `(${baseTimeNano}, ${baseTimeNano}, 'INFO', 'Tool: ${toolName}, Hook: PreToolUse', '${traceId}', '${sqlAttrs(preToolAttrs)}', '{}', '${projectId}', '${toolName}', '${hookSource}', '${sessionId}')`,
      );

      // 3. PostToolUse or PostToolUseFailure event
      const postHookEvent = isFailure ? "PostToolUseFailure" : "PostToolUse";
      const postTimeNano =
        baseTimeNano + BigInt(Math.floor(Math.random() * 5000000)); // 0-5ms later

      const postToolAttrs: Record<string, any> = {
        "gram.event.source": "hook",
        "gram.tool.name": toolName,
        "gram.hook.event": postHookEvent,
        "gram.hook.source": hookSource,
        "gram.project.id": projectId,
        "gen_ai.conversation.id": sessionId,
        "gen_ai.tool_call.id": toolUseId,
      };
      if (userEmail) postToolAttrs["user.email"] = userEmail;
      if (mcpServer && toolName !== "Skill")
        postToolAttrs["gram.tool_call.source"] = mcpServer;
      if (skillName)
        postToolAttrs["gen_ai.tool.call.arguments"] = JSON.stringify({
          skill: skillName,
        });
      if (isFailure) postToolAttrs["gram.hook.error"] = "Tool execution failed";
      else postToolAttrs["gen_ai.tool.call.result"] = "ok";

      chInserts.push(
        `(${postTimeNano}, ${postTimeNano}, '${isFailure ? "ERROR" : "INFO"}', 'Tool: ${toolName}, Hook: ${postHookEvent}', '${traceId}', '${sqlAttrs(postToolAttrs)}', '{}', '${projectId}', '${toolName}', '${hookSource}', '${sessionId}')`,
      );
    }
  }

  const chSQL = `
    SET mutations_sync = 1;
    ALTER TABLE telemetry_logs DELETE WHERE gram_project_id = '${projectId}';
    ALTER TABLE trace_summaries DELETE WHERE gram_project_id = '${projectId}';
    INSERT INTO telemetry_logs (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id, attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id) VALUES
    ${chInserts.join(",\n")};
  `;

  try {
    // Write to temp file and execute via docker
    const tmpFile = `/tmp/seed_clickhouse_${Date.now()}.sql`;
    await fs.writeFile(tmpFile, chSQL);

    // Use docker exec to run clickhouse-client inside the container
    // Copy the file into the container, then execute using --queries-file
    await $`docker cp ${tmpFile} gram-clickhouse-1:/tmp/seed.sql`.quiet();
    await $`docker exec gram-clickhouse-1 clickhouse-client --multiquery --queries-file /tmp/seed.sql`.quiet();
    await fs.unlink(tmpFile);
    log.info(`Inserted ${chInserts.length} telemetry events into ClickHouse`);
  } catch (e) {
    log.warn(`Failed to seed ClickHouse: ${e}`);
  }

  log.info("Observability data seeding complete");
}

function abort(message: string, ...values: unknown[]): never {
  log.error(message);
  for (const value of values) {
    if (typeof value !== "undefined") {
      log.error(
        value instanceof Error ? String(value) : JSON.stringify(value, null, 2),
      );
    }
  }
  process.exit(1);
}

seed();
