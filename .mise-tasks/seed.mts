#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Seed the local database with data"

import assert from "node:assert";
import fs from "node:fs/promises";
import path from "node:path";

import { intro, log, outro } from "@clack/prompts";
import { GramCore } from "@gram/client/core.js";
import { assetsUploadOpenAPIv3 } from "@gram/client/funcs/assetsUploadOpenAPIv3.js";
import { authInfo } from "@gram/client/funcs/authInfo.js";
import { deploymentsEvolveDeployment } from "@gram/client/funcs/deploymentsEvolveDeployment.js";
import { keysCreate } from "@gram/client/funcs/keysCreate.js";
import { keysList } from "@gram/client/funcs/keysList.js";
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
  filename: string;
  storybookDefault?: boolean;
};

const SEED_PROJECTS: {
  name: string;
  slug: string;
  summary: string;
  mcpPublic: boolean;
  assets: Asset[];
}[] = [
  {
    name: "Kitchen Sink",
    slug: "kitchen-sink",
    summary: "An toy API to allow working with Gram Elements",
    mcpPublic: true,
    assets: [
      {
        type: "openapi",
        slug: "kitchen-sink",
        filename: path.join("local", "openapi", "kitchen-sink.json"),
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

  const keyRes = await keysCreate(
    gram,
    {
      createKeyForm: { name: "seed-key", scopes: ["producer"] },
    },
    {
      sessionHeaderGramSession: sessionId,
    },
  );
  if (!keyRes.ok) {
    const listRes = await keysList(gram, undefined, {
      sessionHeaderGramSession: sessionId,
    });
    if (!listRes.ok) {
      abort("Failed to create or list API keys", keyRes.error, listRes.error);
    }
    const existingKey = listRes.value.keys.find((k) => k.name === "seed-key");
    if (!existingKey) {
      abort(`Failed to create API key 'seed-key'`, keyRes.error);
    }

    log.info(`Found existing API key. Continuing...`);
    return;
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
    const spec = await fs.readFile(asset.filename, "utf-8");
    let contentType = "application/json";
    if (asset.filename.endsWith(".yaml")) {
      contentType = "application/x-yaml";
    }

    const res = await assetsUploadOpenAPIv3(
      init.gram,
      {
        contentLength: spec.length,
        requestBody: new Blob([spec], { type: contentType }),
      },
      {
        option2: {
          projectSlugHeaderGramProject: projectSlug,
          sessionHeaderGramSession: sessionId,
        },
      },
    );

    if (!res.ok) {
      abort(`Failed to upload asset \`${asset.filename}\``, res.error);
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
