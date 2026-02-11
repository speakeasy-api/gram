#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Seed the local database with data"

import assert from "node:assert";
import crypto from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

import { intro, log, outro } from "@clack/prompts";
import { GramCore } from "@gram/client/core.js";
import { assetsUploadOpenAPIv3 } from "@gram/client/funcs/assetsUploadOpenAPIv3.js";
import { authInfo } from "@gram/client/funcs/authInfo.js";
import { deploymentsEvolveDeployment } from "@gram/client/funcs/deploymentsEvolveDeployment.js";
import { keysCreate } from "@gram/client/funcs/keysCreate.js";
import { keysList } from "@gram/client/funcs/keysList.js";
import { keysRevokeById } from "@gram/client/funcs/keysRevokeById.js";
import { keysValidate } from "@gram/client/funcs/keysValidate.js";
import { projectsCreate } from "@gram/client/funcs/projectsCreate.js";
import { projectsRead } from "@gram/client/funcs/projectsRead.js";
import { toolsList } from "@gram/client/funcs/toolsList.js";
import { toolsetsCreate } from "@gram/client/funcs/toolsetsCreate.js";
import { toolsetsUpdateBySlug } from "@gram/client/funcs/toolsetsUpdateBySlug.js";
import { ServiceError } from "@gram/client/models/errors";
import { $ } from "zx";

type Asset = {
  type: "openapi";
  slug: string;
  storybookDefault?: boolean;
} & ({ filename: string } | { url: string });

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
    ],
  },
];

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

  const gram = new GramCore({ serverURL });

  const res = await authInfo(gram);
  if (!res.ok) {
    abort("Failed to query session info", res.error);
  }
  const sessionInfo = res.value;
  const sessionJSON = JSON.stringify(sessionInfo, null, 2);
  const sessionHeaders = new Headers(
    Object.entries(sessionInfo.headers).map(([k, vs]): [string, string] => [
      k,
      vs.join(","),
    ]),
  );
  const sessionId = sessionHeaders.get("gram-session");
  if (!sessionId) {
    abort("Session ID not found in session headers", sessionInfo);
  }

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

  const key = await initAPIKey({
    gram,
    sessionId,
  });

  for (const { name, slug, assets, mcpPublic } of SEED_PROJECTS) {
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
    let verb = created ? "Created" : "Found existing";
    log.info(`${verb} project '${projectSlug}' (project_id = ${id})`);

    const deploymentId = await deployAssets({
      gram,
      sessionId,
      projectSlug,
      projectName: name,
      assets,
    });
    log.info(
      `Deployed assets into '${projectSlug}' (deployment_id = ${deploymentId})`,
    );

    for (const asset of assets) {
      const toolset = await upsertToolset({
        gram,
        serverURL,
        sessionId,
        projectSlug,
        assetSlug: asset.slug,
        mcpPublic,
      });
      verb = toolset.created ? "Created" : "Updated";
      log.info(
        `${verb} toolset '${toolset.slug}' for project '${projectSlug}' (mcp_url = ${toolset.mcpURL})`,
      );

      if (asset.storybookDefault) {
        await $`mise set --file mise.local.toml \
        VITE_GRAM_ELEMENTS_STORYBOOK_PROJECT_SLUG=${projectSlug} \
        VITE_GRAM_ELEMENTS_STORYBOOK_MCP_URL=${toolset.mcpURL}`;
      }
    }
  }

  // Seed observability data for the first project
  const firstProject = Object.values(projects)[0];
  if (firstProject) {
    await seedObservabilityData({
      projectId: firstProject.id,
      organizationId: activeOrgID,
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

  for (const asset of assets) {
    let spec: string;
    let contentType: string;

    if ("url" in asset) {
      const response = await fetch(asset.url);
      if (!response.ok) {
        abort(`Failed to fetch OpenAPI spec from ${asset.url}`, response.statusText);
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
  }

  const evolveRes = await deploymentsEvolveDeployment(
    init.gram,
    {
      evolveForm: {
        upsertOpenapiv3Assets: oapi,
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

  return deploymentId;
}

type Toolset = { created: boolean; slug: string; mcpURL: string };

async function upsertToolset(init: {
  gram: GramCore;
  serverURL: string;
  sessionId: string;
  projectSlug: string;
  assetSlug: string;
  mcpPublic: boolean;
}): Promise<Toolset> {
  const { gram, serverURL, sessionId, projectSlug, assetSlug, mcpPublic } =
    init;

  // Fetch tools filtered by URN prefix
  const toolRes = await toolsList(
    gram,
    { urnPrefix: `tools:http:${assetSlug}` },
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

  let toolset: Toolset;
  const name = assetSlug + "-seed";

  const createRes = await toolsetsCreate(
    gram,
    {
      createToolsetRequestBody: {
        name,
        toolUrns: toolUrns,
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
            toolUrns: toolUrns,
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
}): Promise<void> {
  const { projectId, organizationId } = init;

  log.info("Seeding observability data...");

  const NUM_CHATS = 500; // Reduced for faster seeding
  const DAYS_BACK = 30;

  // Tool names for generating realistic data
  const TOOLS = [
    "github:list-repos",
    "slack:send-message",
    "postgres:query",
    "openai:chat",
    "jira:get-ticket",
    "stripe:create-payment",
    "notion:create-page",
  ];

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
    const dbHost = process.env.DB_HOST || "localhost";
    const dbPort = process.env.DB_PORT || "5432";
    const dbUser = process.env.DB_USER || "gram";
    const dbPassword = process.env.DB_PASSWORD || "gram";
    const dbName = process.env.DB_NAME || "gram";

    await $`PGPASSWORD=${dbPassword} psql -h ${dbHost} -p ${dbPort} -U ${dbUser} -d ${dbName} -c ${pgSQL}`.quiet();
    log.info(`Inserted ${NUM_CHATS} chats with messages into PostgreSQL`);
  } catch (e) {
    log.warn(`Failed to seed PostgreSQL: ${e}`);
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

    // Tool call event
    const tool = TOOLS[Math.floor(Math.random() * TOOLS.length)];
    const statusCode = Math.random() < 0.92 ? 200 : [400, 500, 502][Math.floor(Math.random() * 3)];
    const latency = (0.05 + Math.random() * 2).toFixed(3);

    chInserts.push(
      `(${timeNano}, ${timeNano}, 'INFO', 'Tool call: ${tool}', '{"http.response.status_code": ${statusCode}, "http.server.request.duration": ${latency}, "gram.tool.urn": "tools:${tool}", "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}"}', '{"gram.deployment.id": "deployment-1"}', '${projectId}', 'tools:${tool}', 'gram-mcp-gateway', '${chatId}')`,
    );

    // Chat completion event
    const finishReason = Math.random() < 0.65 ? "stop" : Math.random() < 0.9 ? "length" : "error";
    const duration = 30 + Math.floor(Math.random() * 150);
    const completionStatus = Math.random() < 0.92 ? 200 : 500;

    chInserts.push(
      `(${timeNano + BigInt(1000000)}, ${timeNano + BigInt(1000000)}, 'INFO', 'Chat completion', '{"gen_ai.response.finish_reasons": ["${finishReason}"], "gen_ai.conversation.id": "${chatId}", "gen_ai.conversation.duration": ${duration}, "gram.resource.urn": "agents:chat:completion", "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}", "http.response.status_code": ${completionStatus}}', '{"gram.deployment.id": "deployment-1"}', '${projectId}', 'agents:chat:completion', 'gram-mcp-gateway', '${chatId}')`,
    );

    // Resolution event (70% of chats)
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
        `(${timeNano + BigInt(2000000)}, ${timeNano + BigInt(2000000)}, 'INFO', 'Chat resolution: ${resolution}', '{"gen_ai.evaluation.name": "chat_resolution", "gen_ai.evaluation.score.label": "${resolution}", "gen_ai.evaluation.score.value": ${score}, "gen_ai.conversation.id": "${chatId}", "gen_ai.conversation.duration": ${duration}, "gram.project.id": "${projectId}", "user.id": "${userId}", "gram.external_user.id": "${extUserId}", "gram.api_key.id": "${apiKeyId}"}', '{"gram.deployment.id": "deployment-1"}', '${projectId}', 'chat_resolution', 'gram-resolution-analyzer', '${chatId}')`,
      );
    }
  }

  const chSQL = `
    ALTER TABLE telemetry_logs DELETE WHERE gram_project_id = '${projectId}';
    INSERT INTO telemetry_logs (time_unix_nano, observed_time_unix_nano, severity_text, body, attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id) VALUES
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
    log.info(
      `Inserted ${chInserts.length} telemetry events into ClickHouse`,
    );
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
