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

type SeededSkillVersion = {
  label: string;
  pushedAt: string;
  pushedBy: string;
  body: string;
  state: "pending_review" | "active" | "superseded";
  authorName: string;
  firstSeenTraceId: string;
  firstSeenSessionId: string;
  firstSeenAt: string;
};

type SeededSkill = {
  skillUUID: string;
  slug: string;
  name: string;
  description: string;
  createdByUserId: string;
  scope: "project" | "user";
  discoveryRoot:
    | "project_agents"
    | "project_claude"
    | "project_cursor"
    | "user_agents"
    | "user_claude"
    | "user_cursor";
  sourceType: "local_filesystem";
  resolutionStatus:
    | "resolved"
    | "unresolved_name_only"
    | "invalid_skill_root"
    | "skipped_by_author";
  versions: SeededSkillVersion[];
};

type SeededSkillRuntime = {
  name: string;
  skillUUID: string;
  scope: SeededSkill["scope"];
  discoveryRoot: SeededSkill["discoveryRoot"];
  sourceType: SeededSkill["sourceType"];
  resolutionStatus: SeededSkill["resolutionStatus"];
  activeVersionID: string | null;
  latestVersionID: string;
};

const SEEDED_SKILLS: SeededSkill[] = [
  {
    skillUUID: "c73f0446-fdee-5c21-a982-a131b6ee9d4c",
    slug: "engineer-onboarding",
    name: "engineer-onboarding",
    description: "Onboarding skill for new engineering hires",
    createdByUserId: "alice",
    scope: "project",
    discoveryRoot: "project_agents",
    sourceType: "local_filesystem",
    resolutionStatus: "resolved",
    versions: [
      {
        label: "v1.2",
        pushedAt: "2026-03-22T14:00:00Z",
        pushedBy: "alice",
        authorName: "alice",
        state: "active",
        firstSeenTraceId: "seed-trace-engineer-onboarding-v12",
        firstSeenSessionId: "seed-session-engineer-onboarding",
        firstSeenAt: "2026-03-22T14:00:00Z",
        body: "When a new engineer joins the team, walk them through the complete setup process.\n\nKey steps:\n1. Clone the monorepo and run the bootstrap script\n2. Set up local development environment with Docker Compose\n3. Get access to AWS, Datadog, and PagerDuty\n4. Complete the first-week coding challenge\n5. Shadow an on-call rotation",
      },
      {
        label: "v1.1",
        pushedAt: "2026-03-10T10:00:00Z",
        pushedBy: "alice",
        authorName: "alice",
        state: "superseded",
        firstSeenTraceId: "seed-trace-engineer-onboarding-v11",
        firstSeenSessionId: "seed-session-engineer-onboarding",
        firstSeenAt: "2026-03-10T10:00:00Z",
        body: "When a new engineer joins the team, walk them through the setup process.\n\nKey steps:\n1. Clone the monorepo\n2. Set up local development with Docker Compose\n3. Get access to AWS and Datadog\n4. Complete the first-week coding challenge",
      },
      {
        label: "v1.0",
        pushedAt: "2026-02-15T09:00:00Z",
        pushedBy: "alice",
        authorName: "alice",
        state: "superseded",
        firstSeenTraceId: "seed-trace-engineer-onboarding-v10",
        firstSeenSessionId: "seed-session-engineer-onboarding",
        firstSeenAt: "2026-02-15T09:00:00Z",
        body: "When a new engineer joins the team, help them clone the repo, set up local tools, and complete the onboarding challenge.",
      },
    ],
  },
  {
    skillUUID: "9ad6bf0c-1315-54b5-8db2-f59445daad11",
    slug: "incident-response",
    name: "incident-response",
    description:
      "Skill for guiding engineers through incident response procedures",
    createdByUserId: "carol",
    scope: "project",
    discoveryRoot: "project_agents",
    sourceType: "local_filesystem",
    resolutionStatus: "resolved",
    versions: [
      {
        label: "v2.0",
        pushedAt: "2026-03-26T09:00:00Z",
        pushedBy: "carol",
        authorName: "carol",
        state: "active",
        firstSeenTraceId: "seed-trace-incident-response-v20",
        firstSeenSessionId: "seed-session-incident-response",
        firstSeenAt: "2026-03-26T09:00:00Z",
        body: "When an incident is declared, guide the on-call engineer through the response process.\n\nKey procedures:\n1. Acknowledge the alert in PagerDuty within 5 minutes\n2. Open an incident channel in Slack (#inc-YYYYMMDD-brief)\n3. Assess severity using the SEV1-SEV4 framework\n4. Execute the relevant runbook for the affected service\n5. Post status updates every 15 minutes\n6. Conduct a blameless post-mortem within 48 hours",
      },
      {
        label: "v1.0",
        pushedAt: "2026-03-01T12:00:00Z",
        pushedBy: "carol",
        authorName: "carol",
        state: "superseded",
        firstSeenTraceId: "seed-trace-incident-response-v10",
        firstSeenSessionId: "seed-session-incident-response",
        firstSeenAt: "2026-03-01T12:00:00Z",
        body: "When an incident is declared, guide the on-call engineer through acknowledgment, severity assessment, runbook execution, and team communication.",
      },
    ],
  },
  {
    skillUUID: "a319e0bf-dd01-5571-a337-bebe536e374b",
    slug: "competitive-analysis",
    name: "competitive-analysis",
    description: "Skill for answering competitive positioning questions",
    createdByUserId: "alice",
    scope: "project",
    discoveryRoot: "project_claude",
    sourceType: "local_filesystem",
    resolutionStatus: "resolved",
    versions: [
      {
        label: "v3.1",
        pushedAt: "2026-03-26T14:00:00Z",
        pushedBy: "alice",
        authorName: "alice",
        state: "active",
        firstSeenTraceId: "seed-trace-competitive-analysis-v31",
        firstSeenSessionId: "seed-session-competitive-analysis",
        firstSeenAt: "2026-03-26T14:00:00Z",
        body: "When a sales rep asks about competitors, provide accurate and up-to-date competitive intelligence.\n\nKey areas:\n- Feature comparison matrices\n- Pricing intelligence (updated quarterly)\n- Win/loss analysis patterns\n- Competitor weakness talking points\n\nAlways recommend checking the latest landscape doc for current data.",
      },
      {
        label: "v3.0",
        pushedAt: "2026-03-15T11:00:00Z",
        pushedBy: "alice",
        authorName: "alice",
        state: "superseded",
        firstSeenTraceId: "seed-trace-competitive-analysis-v30",
        firstSeenSessionId: "seed-session-competitive-analysis",
        firstSeenAt: "2026-03-15T11:00:00Z",
        body: "When a sales rep asks about competitors, provide current competitive intelligence with pricing, differentiators, and win/loss patterns.",
      },
      {
        label: "v2.0",
        pushedAt: "2026-02-20T08:00:00Z",
        pushedBy: "bob",
        authorName: "bob",
        state: "superseded",
        firstSeenTraceId: "seed-trace-competitive-analysis-v20",
        firstSeenSessionId: "seed-session-competitive-analysis",
        firstSeenAt: "2026-02-20T08:00:00Z",
        body: "Answer competitor questions with battle cards, positioning guidance, and examples from recent deals.",
      },
    ],
  },
  {
    skillUUID: "a77de254-a027-57b2-b9b5-1757bc813132",
    slug: "financial-reporting",
    name: "financial-reporting",
    description: "Skill for generating and interpreting financial reports",
    createdByUserId: "dave",
    scope: "project",
    discoveryRoot: "project_cursor",
    sourceType: "local_filesystem",
    resolutionStatus: "resolved",
    versions: [
      {
        label: "v1.0",
        pushedAt: "2026-03-23T16:00:00Z",
        pushedBy: "dave",
        authorName: "dave",
        state: "active",
        firstSeenTraceId: "seed-trace-financial-reporting-v10",
        firstSeenSessionId: "seed-session-financial-reporting",
        firstSeenAt: "2026-03-23T16:00:00Z",
        body: "Help finance team members with quarterly and annual financial reporting.\n\nKey capabilities:\n- Revenue recognition calculations (ASC 606)\n- ARR/MRR breakdown by customer segment\n- Churn and expansion metrics\n- Board deck financial slide preparation\n- Variance analysis against forecast",
      },
    ],
  },
  {
    skillUUID: "261a536d-8653-52e7-8e46-10100d458036",
    slug: "objection-handling",
    name: "objection-handling",
    description:
      "Skill for handling common sales objections with proven responses",
    createdByUserId: "cursor-agent-12",
    scope: "user",
    discoveryRoot: "user_cursor",
    sourceType: "local_filesystem",
    resolutionStatus: "unresolved_name_only",
    versions: [
      {
        label: "pending-review",
        pushedAt: "2026-03-31T09:00:00Z",
        pushedBy: "cursor-agent-12",
        authorName: "cursor-agent-12",
        state: "pending_review",
        firstSeenTraceId: "seed-trace-objection-handling-pr",
        firstSeenSessionId: "seed-session-objection-handling",
        firstSeenAt: "2026-03-31T09:00:00Z",
        body: "When a prospect raises an objection during a sales call, provide the recommended response framework.\n\nCommon objections:\n- Price concerns: reframe around TCO and ROI calculator\n- Existing solution: focus on real-time capabilities and scale\n- On-prem requirements: highlight hybrid deployment option\n- Security concerns: reference SOC 2 certification and CISO call",
      },
    ],
  },
  {
    skillUUID: "bc4e7a97-4e72-5b91-8140-ee6e3848cf3f",
    slug: "compliance-checker",
    name: "compliance-checker",
    description:
      "Automated compliance verification for SOC 2, GDPR, and data retention policies",
    createdByUserId: "dave",
    scope: "project",
    discoveryRoot: "project_agents",
    sourceType: "local_filesystem",
    resolutionStatus: "resolved",
    versions: [
      {
        label: "v1.3",
        pushedAt: "2026-04-01T10:00:00Z",
        pushedBy: "dave",
        authorName: "dave",
        state: "active",
        firstSeenTraceId: "seed-trace-compliance-checker-v13",
        firstSeenSessionId: "seed-session-compliance-checker",
        firstSeenAt: "2026-04-01T10:00:00Z",
        body: "Verify compliance posture across the organization.\n\nCapabilities:\n- Check SOC 2 Type II control status\n- Validate GDPR data processing agreements for EU customers\n- Audit data retention policy adherence\n- Generate compliance summary for board reporting\n- Flag overdue DPA renewals\n- Cross-reference audit log retention with regulatory requirements",
      },
      {
        label: "v1.2",
        pushedAt: "2026-03-20T14:00:00Z",
        pushedBy: "dave",
        authorName: "dave",
        state: "superseded",
        firstSeenTraceId: "seed-trace-compliance-checker-v12",
        firstSeenSessionId: "seed-session-compliance-checker",
        firstSeenAt: "2026-03-20T14:00:00Z",
        body: "Verify compliance posture across the organization.\n\nCapabilities:\n- Check SOC 2 Type II control status\n- Validate GDPR data processing agreements for EU customers\n- Audit data retention policy adherence\n- Generate compliance summary for board reporting",
      },
    ],
  },
];

const SEEDED_CAPTURE_MODE = "project_and_user";

const SEEDED_SKILL_TRACES = [
  {
    traceId: "seedskilltrace000000000000000001",
    skillUUID: "c73f0446-fdee-5c21-a982-a131b6ee9d4c",
    sessionId: "seed-skill-session-001",
    hookSource: "claude",
    userEmail: "alice@example.com",
    timestamp: "2026-04-12T09:00:00Z",
    success: true,
  },
  {
    traceId: "seedskilltrace000000000000000002",
    skillUUID: "c73f0446-fdee-5c21-a982-a131b6ee9d4c",
    sessionId: "seed-skill-session-002",
    hookSource: "cli",
    userEmail: "bob@example.com",
    timestamp: "2026-04-12T09:05:00Z",
    success: true,
  },
  {
    traceId: "seedskilltrace000000000000000003",
    skillUUID: "9ad6bf0c-1315-54b5-8db2-f59445daad11",
    sessionId: "seed-skill-session-003",
    hookSource: "claude",
    userEmail: "carol@example.com",
    timestamp: "2026-04-12T09:10:00Z",
    success: true,
  },
  {
    traceId: "seedskilltrace000000000000000004",
    skillUUID: "a319e0bf-dd01-5571-a337-bebe536e374b",
    sessionId: "seed-skill-session-004",
    hookSource: "vscode",
    userEmail: "alice@example.com",
    timestamp: "2026-04-12T09:15:00Z",
    success: true,
  },
  {
    traceId: "seedskilltrace000000000000000005",
    skillUUID: "a319e0bf-dd01-5571-a337-bebe536e374b",
    sessionId: "seed-skill-session-005",
    hookSource: "api",
    userEmail: "bob@example.com",
    timestamp: "2026-04-12T09:20:00Z",
    success: true,
  },
  {
    traceId: "seedskilltrace000000000000000006",
    skillUUID: "a77de254-a027-57b2-b9b5-1757bc813132",
    sessionId: "seed-skill-session-006",
    hookSource: "cli",
    userEmail: "dave@example.com",
    timestamp: "2026-04-12T09:25:00Z",
    success: true,
  },
  {
    traceId: "seedskilltrace000000000000000007",
    skillUUID: "bc4e7a97-4e72-5b91-8140-ee6e3848cf3f",
    sessionId: "seed-skill-session-007",
    hookSource: "claude",
    userEmail: "dave@example.com",
    timestamp: "2026-04-12T09:30:00Z",
    success: true,
  },
  {
    traceId: "seedskilltrace000000000000000008",
    skillUUID: "261a536d-8653-52e7-8e46-10100d458036",
    sessionId: "seed-skill-session-008",
    hookSource: "vscode",
    userEmail: "carol@example.com",
    timestamp: "2026-04-12T09:35:00Z",
    success: false,
  },
] as const;

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
    const seededSkills = await seedSkillsData({
      projectId: firstProject.id,
      organizationId: activeOrgID,
      serverURL,
    });
    const toolUrns = projectToolUrns[firstProject.slug] ?? [];
    await seedObservabilityData({
      projectId: firstProject.id,
      organizationId: activeOrgID,
      toolUrns,
      seededSkills,
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
      const requiredScopes = new Set(["producer", "chat", "hooks"]);
      const existingScopes = new Set(vres.value.scopes ?? []);
      const missingScopes = [...requiredScopes].filter(
        (scope) => !existingScopes.has(scope),
      );

      if (missingScopes.length === 0) {
        log.info(`Using existing GRAM_API_KEY environment variable.`);
        return;
      }

      log.warn(
        `Existing GRAM_API_KEY is missing required scopes: ${missingScopes.join(", ")}. Creating a new API key...`,
      );
    } else {
      log.warn(`Existing GRAM_API_KEY is invalid. Creating a new API key...`);
    }
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
      createKeyForm: {
        name: "seed-key",
        scopes: ["producer", "chat", "hooks"],
      },
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

// The 11 Gram API tools that compose the built-in MCP Logs server.
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

// Namespace UUID for generating deterministic seed IDs
const CHAT_UUID_NAMESPACE = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"; // DNS namespace
const SEED_DEPLOYMENT_ID = "d17a5eed-0001-5000-9000-000000000001";

function generateNamespacedUUID(value: string): string {
  const hash = crypto
    .createHash("sha1")
    .update(CHAT_UUID_NAMESPACE)
    .update(value)
    .digest();

  // Set version (5) and variant bits
  hash[6] = (hash[6] & 0x0f) | 0x50;
  hash[8] = (hash[8] & 0x3f) | 0x80;

  const hex = hash.toString("hex").slice(0, 32);
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`;
}

function generateChatUUID(chatNumber: number): string {
  return generateNamespacedUUID(`chat-${chatNumber}`);
}

function seedUUID(prefix: string, index: number): string {
  return generateNamespacedUUID(`${prefix}-${index}`);
}

function sqlString(value: string): string {
  return `'${value.replace(/'/g, "''")}'`;
}

function sqlNullable(value: string | null | undefined): string {
  return value == null ? "NULL" : sqlString(value);
}

function sqlTimestamp(value: string): string {
  return `${sqlString(value)}::timestamptz`;
}

function escapeClickHouseJSON(value: unknown): string {
  return JSON.stringify(value).replace(/\\/g, "\\\\").replace(/'/g, "\\'");
}

async function executePostgresSQL(label: string, sql: string): Promise<void> {
  const dbUser = process.env.DB_USER || "gram";
  const dbName = process.env.DB_NAME || "gram";
  const tmpFile = path.join(process.cwd(), `.seed-${label}.sql`);

  await fs.writeFile(tmpFile, sql, "utf-8");

  try {
    await $`docker compose cp ${tmpFile} gram-db:/tmp/${label}.sql`.quiet();
    await $`docker compose exec gram-db psql -v ON_ERROR_STOP=1 -U ${dbUser} -d ${dbName} -f /tmp/${label}.sql`.quiet();
  } finally {
    await fs.unlink(tmpFile).catch(() => {});
  }
}

async function seedSkillsData(init: {
  projectId: string;
  organizationId: string;
  serverURL: string;
}): Promise<SeededSkillRuntime[]> {
  const { projectId, organizationId, serverURL } = init;
  const skillSlugs = SEEDED_SKILLS.map((skill) => sqlString(skill.slug)).join(
    ", ",
  );
  const skillUUIDs = SEEDED_SKILLS.map((skill) =>
    sqlString(skill.skillUUID),
  ).join(", ");
  const statements: string[] = [];
  const runtimeSkills: SeededSkillRuntime[] = [];

  statements.push("BEGIN;");
  statements.push(`
    DELETE FROM skill_versions
    WHERE skill_id IN (
      SELECT id
      FROM skills
      WHERE project_id = ${sqlString(projectId)}::uuid
        AND (
          slug IN (${skillSlugs})
          OR skill_uuid IN (${skillUUIDs})
        )
    );
  `);
  statements.push(`
    DELETE FROM skills
    WHERE project_id = ${sqlString(projectId)}::uuid
      AND (
        slug IN (${skillSlugs})
        OR skill_uuid IN (${skillUUIDs})
      );
  `);
  statements.push(`
    DELETE FROM assets
    WHERE project_id = ${sqlString(projectId)}::uuid
      AND kind = 'skill'
      AND name LIKE 'seed-skill-%';
  `);
  statements.push(`
    INSERT INTO skills_capture_policies (
        organization_id,
        project_id,
        mode
    ) VALUES (
        ${sqlString(organizationId)},
        NULL,
        ${sqlString(SEEDED_CAPTURE_MODE)}
    )
    ON CONFLICT (organization_id)
    WHERE project_id IS NULL AND deleted IS FALSE
    DO UPDATE SET
        mode = EXCLUDED.mode,
        deleted_at = NULL,
        updated_at = clock_timestamp();
  `);

  for (const skill of SEEDED_SKILLS) {
    const skillID = generateNamespacedUUID(
      `seed-skill:${projectId}:${skill.skillUUID}`,
    );
    const latestVersion = skill.versions[0];
    const activeVersion =
      skill.versions.find((version) => version.state === "active") ?? null;
    const activeVersionID = activeVersion
      ? generateNamespacedUUID(
          `seed-skill-version:${projectId}:${skill.skillUUID}:${activeVersion.label}`,
        )
      : null;

    statements.push(`
      INSERT INTO skills (
          id,
          organization_id,
          project_id,
          name,
          slug,
          description,
          skill_uuid,
          active_version_id,
          created_by_user_id,
          created_at,
          updated_at
      ) VALUES (
          ${sqlString(skillID)}::uuid,
          ${sqlString(organizationId)},
          ${sqlString(projectId)}::uuid,
          ${sqlString(skill.name)},
          ${sqlString(skill.slug)},
          ${sqlString(skill.description)},
          ${sqlString(skill.skillUUID)},
          NULL,
          ${sqlString(skill.createdByUserId)},
          ${sqlTimestamp(latestVersion.pushedAt)},
          ${sqlTimestamp(latestVersion.pushedAt)}
      );
    `);

    for (const version of skill.versions) {
      const versionID = generateNamespacedUUID(
        `seed-skill-version:${projectId}:${skill.skillUUID}:${version.label}`,
      );
      const assetID = generateNamespacedUUID(
        `seed-skill-asset:${projectId}:${skill.skillUUID}:${version.label}`,
      );
      const skillBytes = Buffer.byteLength(version.body, "utf8");
      const contentSHA256 = crypto
        .createHash("sha256")
        .update(version.body)
        .digest("hex");
      const assetURL = `${serverURL}/seed-assets/skills/${skill.slug}/${version.label}.zip`;

      statements.push(`
        INSERT INTO assets (
            id,
            project_id,
            name,
            url,
            kind,
            content_type,
            content_length,
            sha256,
            created_at,
            updated_at
        ) VALUES (
            ${sqlString(assetID)}::uuid,
            ${sqlString(projectId)}::uuid,
            ${sqlString(`seed-skill-${skill.slug}-${version.label}.zip`)},
            ${sqlString(assetURL)},
            'skill',
            'application/zip',
            ${skillBytes},
            ${sqlString(contentSHA256)},
            ${sqlTimestamp(version.pushedAt)},
            ${sqlTimestamp(version.pushedAt)}
        );
      `);

      statements.push(`
        INSERT INTO skill_versions (
            id,
            skill_id,
            asset_id,
            content_sha256,
            asset_format,
            size_bytes,
            skill_bytes,
            state,
            captured_by_user_id,
            author_name,
            first_seen_trace_id,
            first_seen_session_id,
            first_seen_at,
            created_at,
            updated_at
        ) VALUES (
            ${sqlString(versionID)}::uuid,
            ${sqlString(skillID)}::uuid,
            ${sqlString(assetID)}::uuid,
            ${sqlString(contentSHA256)},
            'zip',
            ${skillBytes},
            ${skillBytes},
            ${sqlString(version.state)},
            ${sqlString(skill.createdByUserId)},
            ${sqlString(version.authorName)},
            ${sqlString(version.firstSeenTraceId)},
            ${sqlString(version.firstSeenSessionId)},
            ${sqlTimestamp(version.firstSeenAt)},
            ${sqlTimestamp(version.pushedAt)},
            ${sqlTimestamp(version.pushedAt)}
        );
      `);
    }

    if (activeVersionID) {
      statements.push(`
        UPDATE skills
        SET active_version_id = ${sqlString(activeVersionID)}::uuid,
            updated_at = ${sqlTimestamp(latestVersion.pushedAt)}
        WHERE id = ${sqlString(skillID)}::uuid;
      `);
    }

    runtimeSkills.push({
      name: skill.name,
      skillUUID: skill.skillUUID,
      scope: skill.scope,
      discoveryRoot: skill.discoveryRoot,
      sourceType: skill.sourceType,
      resolutionStatus: skill.resolutionStatus,
      activeVersionID,
      latestVersionID: generateNamespacedUUID(
        `seed-skill-version:${projectId}:${skill.skillUUID}:${latestVersion.label}`,
      ),
    });
  }

  statements.push("COMMIT;");
  await executePostgresSQL("skills", statements.join("\n"));
  log.info(`Seeded ${SEEDED_SKILLS.length} skills into PostgreSQL`);

  return runtimeSkills;
}

async function seedObservabilityData(init: {
  projectId: string;
  organizationId: string;
  toolUrns: string[];
  seededSkills: SeededSkillRuntime[];
}): Promise<void> {
  const { projectId, organizationId, toolUrns, seededSkills } = init;

  log.info(`Seeding observability data with ${toolUrns.length} tool URNs...`);

  if (toolUrns.length === 0) {
    log.warn(
      "No tool URNs available for seeding observability data. Skipping.",
    );
    return;
  }

  const NUM_CHATS = 500; // Reduced for faster seeding
  const DAYS_BACK = 30;
  const NUM_HOOKS = 500; // Reduced for faster seeding

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
        user_id: seedUUID("user", Math.floor(Math.random() * 1000)),
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
    const extUserId = seedUUID("ext-user", i % 80);
    const userId = seedUUID("user", i % 200);

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
    const extUserId = seedUUID("ext-user", i % 80);
    const userId = seedUUID("user", i % 200);
    const apiKeyId = seedUUID("key", i % 5);

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
      `(${timeNano}, ${timeNano}, 'INFO', 'Tool call: ${toolUrn}', '${traceId}', '{"http.response.status_code": ${statusCode}, "http.server.request.duration": ${latency}, "gram.tool.urn": "${toolUrn}", "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}"}', '{"gram.deployment.id": "${SEED_DEPLOYMENT_ID}"}', '${projectId}', '${toolUrn}', 'gram-mcp-gateway', '${chatId}')`,
    );

    // Chat completion event - same trace ID links it to the tool call
    const finishReason =
      Math.random() < 0.65 ? "stop" : Math.random() < 0.9 ? "length" : "error";
    const duration = 30 + Math.floor(Math.random() * 150);
    const completionStatus = Math.random() < 0.92 ? 200 : 500;

    chInserts.push(
      `(${timeNano + BigInt(1000000)}, ${timeNano + BigInt(1000000)}, 'INFO', 'Chat completion', '${traceId}', '{"gen_ai.response.finish_reasons": ["${finishReason}"], "gen_ai.conversation.id": "${chatId}", "gen_ai.conversation.duration": ${duration}, "gram.resource.urn": "agents:chat:completion", "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}", "http.response.status_code": ${completionStatus}}', '{"gram.deployment.id": "${SEED_DEPLOYMENT_ID}"}', '${projectId}', 'agents:chat:completion', 'gram-mcp-gateway', '${chatId}')`,
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
        `(${timeNano + BigInt(2000000)}, ${timeNano + BigInt(2000000)}, 'INFO', 'Chat resolution: ${resolution}', '${traceId}', '{"gen_ai.evaluation.name": "chat_resolution", "gen_ai.evaluation.score.label": "${resolution}", "gen_ai.evaluation.score.value": ${score}, "gen_ai.conversation.id": "${chatId}", "gen_ai.conversation.duration": ${duration}, "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}"}', '{"gram.deployment.id": "${SEED_DEPLOYMENT_ID}"}', '${projectId}', 'chat_resolution', 'gram-resolution-analyzer', '${chatId}')`,
      );
    }
  }

  // Hook-specific constants
  const HOOK_SOURCES = ["claude", "vscode", "cli", "api"];
  const MCP_SERVERS = ["", "github", "filesystem", "postgres", "slack", ""]; // Empty string = local tools
  const HOOK_TOOL_NAMES = [
    "Read",
    "Write",
    "Edit",
    "Bash",
    "Grep",
    "Glob",
    "mcp__github__list-repos",
    "mcp__filesystem__read-file",
    "mcp__postgres__query",
  ];
  const USER_EMAILS = [
    "alice@example.com",
    "bob@example.com",
    "charlie@example.com",
    "",
  ]; // Empty string = no user email

  for (let i = 0; i < NUM_HOOKS; i++) {
    const sessionId = seedUUID("session", i % 50); // Group hooks into sessions
    const toolUseId = `toolu_${crypto.randomBytes(12).toString("hex")}`;
    const userEmail =
      USER_EMAILS[Math.floor(Math.random() * USER_EMAILS.length)];
    const hookSource =
      HOOK_SOURCES[Math.floor(Math.random() * HOOK_SOURCES.length)];
    const toolName =
      HOOK_TOOL_NAMES[Math.floor(Math.random() * HOOK_TOOL_NAMES.length)];
    const mcpServer =
      MCP_SERVERS[Math.floor(Math.random() * MCP_SERVERS.length)];

    const daysAgo = Math.random() * DAYS_BACK;
    const eventTime = new Date(now - daysAgo * msPerDay);
    const baseTimeNano = BigInt(eventTime.getTime()) * BigInt(1000000);

    // Generate a unique trace ID for this tool call (32 hex chars)
    const traceId = crypto.randomBytes(16).toString("hex");

    // Decide if this is a successful call or failure.
    const isFailure = Math.random() > 0.9;

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
        `(${baseTimeNano}, ${baseTimeNano}, 'INFO', 'Hook: SessionStart', '${traceId}', '${JSON.stringify(attrs).replace(/'/g, "\\'")}', '{}', '${projectId}', 'SessionStart', '${hookSource}', '${sessionId}')`,
      );
    } else {
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
      if (mcpServer) preToolAttrs["gram.tool_call.source"] = mcpServer;

      chInserts.push(
        `(${baseTimeNano}, ${baseTimeNano}, 'INFO', 'Tool: ${toolName}, Hook: PreToolUse', '${traceId}', '${JSON.stringify(preToolAttrs).replace(/'/g, "\\'")}', '{}', '${projectId}', '${toolName}', '${hookSource}', '${sessionId}')`,
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
      if (mcpServer) postToolAttrs["gram.tool_call.source"] = mcpServer;
      if (isFailure) postToolAttrs["gram.hook.error"] = "Tool execution failed";

      chInserts.push(
        `(${postTimeNano}, ${postTimeNano}, '${isFailure ? "ERROR" : "INFO"}', 'Tool: ${toolName}, Hook: ${postHookEvent}', '${traceId}', '${JSON.stringify(postToolAttrs).replace(/'/g, "\\'")}', '{}', '${projectId}', '${toolName}', '${hookSource}', '${sessionId}')`,
      );
    }
  }

  const seededSkillByUUID = new Map(
    seededSkills.map((skill) => [skill.skillUUID, skill] as const),
  );

  for (const trace of SEEDED_SKILL_TRACES) {
    const seededSkill = seededSkillByUUID.get(trace.skillUUID);
    if (!seededSkill) {
      continue;
    }

    const baseTimeNano = BigInt(Date.parse(trace.timestamp)) * BigInt(1000000);
    const versionID =
      seededSkill.activeVersionID ?? seededSkill.latestVersionID;
    const preToolAttrs = {
      "gram.event.source": "hook",
      "gram.tool.name": "Skill",
      "gram.hook.event": "PreToolUse",
      "gram.hook.source": trace.hookSource,
      "gram.project.id": projectId,
      "gen_ai.conversation.id": trace.sessionId,
      "gen_ai.tool_call.id": `toolu_${trace.traceId}`,
      "user.email": trace.userEmail,
      "gram.skill.scope": seededSkill.scope,
      "gram.skill.discovery_root": seededSkill.discoveryRoot,
      "gram.skill.source_type": seededSkill.sourceType,
      "gram.skill.id": seededSkill.skillUUID,
      "gram.skill.version_id": versionID,
      "gram.skill.resolution_status": seededSkill.resolutionStatus,
      "gen_ai.tool.call.arguments": JSON.stringify({
        skill: seededSkill.name,
      }),
    };
    const postToolAttrs = {
      ...preToolAttrs,
      "gram.hook.event": trace.success ? "PostToolUse" : "PostToolUseFailure",
      ...(trace.success
        ? {}
        : { "gram.hook.error": "Seeded skill invocation failed" }),
    };

    chInserts.push(
      `(${baseTimeNano}, ${baseTimeNano}, 'INFO', 'Tool: Skill, Hook: PreToolUse', '${trace.traceId}', '${escapeClickHouseJSON(preToolAttrs)}', '{}', '${projectId}', 'Skill', '${trace.hookSource}', '${trace.sessionId}')`,
    );
    chInserts.push(
      `(${baseTimeNano + BigInt(1000000)}, ${baseTimeNano + BigInt(1000000)}, '${trace.success ? "INFO" : "ERROR"}', 'Tool: Skill, Hook: ${trace.success ? "PostToolUse" : "PostToolUseFailure"}', '${trace.traceId}', '${escapeClickHouseJSON(postToolAttrs)}', '{}', '${projectId}', 'Skill', '${trace.hookSource}', '${trace.sessionId}')`,
    );
  }

  const chSQL = `
    ALTER TABLE telemetry_logs DELETE WHERE gram_project_id = '${projectId}';
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
