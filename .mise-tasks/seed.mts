#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Seed the local database with data"

import path from "node:path";
import fs from "node:fs/promises";

const PROJECTS: {
  name: string;
  slug: string;
  summary: string;
  openapi: string;
  url: string;
  image: string;
}[] = [
  {
    name: "Dub",
    slug: "dub",
    summary: "The modern link management platform for teams",
    openapi: path.join("local", "openapi", "dub.json"),
    url: "https://dub.co",
    image: path.join("local", "openapi", "dub.png"),
  },
  {
    name: "HubSpot",
    slug: "hubspot",
    summary:
      "AI-powered CRM for inbound marketing, sales, and customer service",
    openapi: path.join("local", "openapi", "hubspot.json"),
    url: "https://hubspot.com",
    image: path.join("local", "openapi", "hubspot.png"),
  },
  {
    name: "Polar",
    slug: "polar",
    summary: "Payment infrastructure for the 21st century",
    openapi: path.join("local", "openapi", "polar.json"),
    url: "https://polar.sh",
    image: path.join("local", "openapi", "polar.png"),
  },
  {
    name: "Speakeasy",
    slug: "speakeasy",
    summary: "Generate SDKs, docs, and agent tools using your API",
    openapi: path.join("local", "openapi", "speakeasy.yaml"),
    url: "https://speakeasy.com",
    image: path.join("local", "openapi", "speakeasy.png"),
  },
];

async function run() {
  const [chost, cport] = process.env["GRAM_CONTROL_ADDRESS"]?.split(":") ?? [];
  if (!cport) {
    throw new Error("GRAM_CONTROL_ADDRESS is not set");
  }

  const [host, port] = process.env["GRAM_SERVER_ADDRESS"]?.split(":") ?? [];
  if (!cport) {
    throw new Error("GRAM_SERVER_ADDRESS is not set");
  }

  const liveRes = await fetch(
    `http://${chost || "localhost"}:${cport}/healthz`
  );
  if (!liveRes.ok) {
    await logBadResponse(liveRes);
    process.exit(1);
  }

  const base = `http://${host || "localhost"}:${port}`;

  const sessionRes = await fetch(`${base}/rpc/auth.info`);
  if (!sessionRes.ok) {
    await logBadResponse(sessionRes);
    process.exit(1);
  }

  const sessionInfo = await sessionRes.json();
  const sessionJSON = JSON.stringify(sessionInfo, null, 2);
  const sessionId = sessionRes.headers.get("gram-session");
  if (!sessionId) {
    console.error("Session ID not found");
    console.error(sessionJSON);
    process.exit(1);
  }

  const activeOrgID = sessionInfo?.active_organization_id;
  if (typeof activeOrgID !== "string" || !activeOrgID) {
    console.error("Active organization not found");
    console.error(sessionJSON);
    process.exit(1);
  }

  const orgs = sessionInfo?.organizations;
  if (!Array.isArray(orgs)) {
    console.error("Organizations list not found");
    console.error(sessionJSON);
    process.exit(1);
  }

  const org = orgs.find(
    (o: unknown) =>
      typeof o === "object" && o != null && "id" in o && o?.id === activeOrgID
  );
  if (!org) {
    console.error("Active organization details not found");
    console.error(sessionJSON);
    process.exit(1);
  }

  const rawProjects = org.projects;
  if (!Array.isArray(rawProjects)) {
    console.error("Projects list not found");
    console.error(sessionJSON);
    process.exit(1);
  }

  const projects: Record<string, { slug: string; id: string }> = {};
  for (const p of rawProjects) {
    if (typeof p !== "object" || p == null) {
      console.error("Project details not found");
      console.error(sessionJSON);
      process.exit(1);
    }

    const id = p.id;
    const slug = p.slug;

    if (typeof id !== "string" || typeof slug !== "string") {
      console.error("Project details are missing slug and id fields");
      console.error(sessionJSON);
      process.exit(1);
    }

    projects[slug] = { id, slug };
  }

  console.group("Current projects:");
  for (const [slug, { id }] of Object.entries(projects)) {
    console.log(`- ${slug} (${id})`);
  }
  if (Object.keys(projects).length === 0) {
    console.log("<EMPTY>");
  }
  console.groupEnd();
  console.log("---\n");

  for (const { name, slug, openapi, image, summary, url } of PROJECTS) {
    if (slug in projects) {
      continue;
    }

    const { id, slug: newSlug } = await createProject({
      base,
      sessionId,
      activeOrgID,
      name,
      slug,
    });
    projects[newSlug] = { id, slug: newSlug };
    console.log(`Created project ${name} \`${slug}\` (${id})`);

    const deploymentId = await deployOpenAPIv3({
      base,
      sessionId,
      projectSlug: slug,
      projectName: name,
      filename: openapi,
    });
    console.log(`Deployed ${openapi} into \`${name}\` (${deploymentId})`);

    const { id: pkgId, name: pkgName } = await createPackage({
      base,
      sessionId,
      projectSlug: slug,
      name: slug,
      title: name,
      summary: summary,
      url: url,
      image: image,
    });
    projects[newSlug] = { id, slug: newSlug };
    console.log(`Created package \`${pkgName}\` (${pkgId})`);

    const { id: pkgVersionId, semver } = await publishPackage({
      base,
      sessionId,
      projectSlug: slug,
      packageName: pkgName,
      deploymentId,
      version: "1.0.0",
      visibility: "public",
    });
    console.log(
      `Published package version \`${pkgName}@${semver}\` (${pkgVersionId})`
    );
    console.log("---");
  }
}

async function createProject(init: {
  base: string;
  sessionId: string;
  activeOrgID: string;
  name: string;
  slug: string;
}): Promise<{ id: string; slug: string }> {
  const { base: baseURL, sessionId, activeOrgID, name, slug } = init;
  const res = await fetch(`${baseURL}/rpc/projects.create`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Gram-Session": sessionId ?? "",
    },
    body: JSON.stringify({ name, slug, organization_id: activeOrgID }),
  });
  if (!res.ok) {
    await logBadResponse(res);
    process.exit(1);
  }

  const body = await res.json();
  const bodyJSON = JSON.stringify(body, null, 2);
  if (
    !("project" in body) ||
    typeof body.project !== "object" ||
    body.project == null
  ) {
    console.error(`Project details not found: ${name} (${slug})`);
    console.error(bodyJSON);
    process.exit(1);
  }

  const project = body.project;
  const { id, slug: newSlug } = project;
  if (typeof id !== "string" || typeof newSlug !== "string") {
    console.error(
      `Project details are missing slug and id fields: ${name} (${slug})`
    );
    console.error(bodyJSON);
    process.exit(1);
  }

  return { id, slug: newSlug };
}

async function deployOpenAPIv3(init: {
  base: string;
  sessionId: string;
  projectSlug: string;
  projectName: string;
  filename: string;
}): Promise<string> {
  const { base, sessionId, projectSlug, projectName, filename } = init;

  const spec = await fs.readFile(filename, "utf-8");
  let contentType = "application/json";
  if (filename.endsWith(".yaml")) {
    contentType = "application/x-yaml";
  }

  const assetRes = await fetch(`${base}/rpc/assets.uploadOpenAPIv3`, {
    method: "POST",
    headers: {
      "Content-Type": contentType,
      "Gram-Session": sessionId ?? "",
      "Gram-Project": projectSlug,
    },
    body: spec,
  });
  if (!assetRes.ok) {
    await logBadResponse(assetRes);
    process.exit(1);
  }

  let body = await assetRes.json();
  let bodyJSON = JSON.stringify(body, null, 2);
  if (
    !("asset" in body) ||
    typeof body.asset !== "object" ||
    body.asset == null
  ) {
    console.error("Asset details not found");
    console.error(bodyJSON);
    process.exit(1);
  }

  const assetId = body.asset.id;
  if (typeof assetId !== "string" || !assetId) {
    console.error("Asset ID not found");
    console.error(bodyJSON);
    process.exit(1);
  }

  const evolveRes = await fetch(`${base}/rpc/deployments.evolve`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Gram-Session": sessionId ?? "",
      "Gram-Project": projectSlug,
    },
    body: JSON.stringify({
      upsert_openapiv3_assets: [
        {
          asset_id: assetId,
          name: projectName,
          slug: projectSlug,
        },
      ],
    }),
  });
  if (!evolveRes.ok) {
    await logBadResponse(evolveRes);
    process.exit(1);
  }

  body = await evolveRes.json();
  bodyJSON = JSON.stringify(body, null, 2);
  if (
    !("deployment" in body) ||
    typeof body.deployment !== "object" ||
    body.deployment == null
  ) {
    console.error("Deployment details not found");
    console.error(bodyJSON);
    process.exit(1);
  }

  const deploymentId = body.deployment.id;
  if (typeof deploymentId !== "string" || !deploymentId) {
    console.error("Deployment ID not found");
    console.error(bodyJSON);
    process.exit(1);
  }

  return deploymentId;
}

async function createPackage(init: {
  base: string;
  sessionId: string;
  projectSlug: string;
  title: string;
  summary: string;
  name: string;
  url: string;
  image: string;
}): Promise<{ id: string; name: string; title: string }> {
  const {
    base: baseURL,
    sessionId,
    projectSlug,
    name,
    title,
    summary,
    image,
    url,
  } = init;

  //eslint-disable-next-line @typescript-eslint/no-unsafe-assignment  
  const imageRes = await fetch(`${baseURL}/rpc/assets.uploadImage`, {
    method: "POST",
    headers: {
      "Content-Type": "image/png",
      "Gram-Session": sessionId ?? "",
      "Gram-Project": projectSlug,
    },
    body: await fs.readFile(image) as any, // This is causing a type error
  });
  if (!imageRes.ok) {
    await logBadResponse(imageRes);
    process.exit(1);
  }

  const imageBody = await imageRes.json();
  const imageBodyJSON = JSON.stringify(imageBody, null, 2);
  if (
    !("asset" in imageBody) ||
    typeof imageBody.asset !== "object" ||
    imageBody.asset == null
  ) {
    console.error("Image asset details not found");
    console.error(imageBodyJSON);
    process.exit(1);
  }

  const assetId = imageBody.asset.id;
  if (typeof assetId !== "string" || !assetId) {
    console.error("Asset ID not found");
    console.error(imageBodyJSON);
    process.exit(1);
  }

  const res = await fetch(`${baseURL}/rpc/packages.create`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Gram-Session": sessionId ?? "",
      "Gram-Project": projectSlug,
    },
    body: JSON.stringify({
      name,
      title,
      summary,
      url,
      image_asset_id: assetId,
    }),
  });
  if (!res.ok) {
    await logBadResponse(res);
    process.exit(1);
  }

  const body = await res.json();
  const bodyJSON = JSON.stringify(body, null, 2);
  if (
    !("package" in body) ||
    typeof body.package !== "object" ||
    body.package == null
  ) {
    console.error(`Package details not found: ${name}`);
    console.error(bodyJSON);
    process.exit(1);
  }

  const pkg = body.package;
  const { id, name: newName } = pkg;
  if (typeof id !== "string" || typeof newName !== "string") {
    console.error(`Project details are missing slug and id fields: ${name}`);
    console.error(bodyJSON);
    process.exit(1);
  }

  return { id, title, name: newName };
}

async function publishPackage(init: {
  base: string;
  sessionId: string;
  projectSlug: string;
  packageName: string;
  deploymentId: string;
  version: string;
  visibility: "public" | "private";
}): Promise<{
  id: string;
  name: string;
  deploymentId: string;
  semver: string;
}> {
  const {
    base: baseURL,
    sessionId,
    projectSlug,
    packageName,
    deploymentId,
    version,
    visibility,
  } = init;
  const res = await fetch(`${baseURL}/rpc/packages.publish`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Gram-Session": sessionId ?? "",
      "Gram-Project": projectSlug,
    },
    body: JSON.stringify({
      name: packageName,
      version,
      deployment_id: deploymentId,
      visibility,
    }),
  });
  if (!res.ok) {
    await logBadResponse(res);
    process.exit(1);
  }

  const body = await res.json();
  const bodyJSON = JSON.stringify(body, null, 2);
  if (
    !("package" in body) ||
    typeof body.package !== "object" ||
    body.package == null
  ) {
    console.error(`Package details not found: ${name}`);
    console.error(bodyJSON);
    process.exit(1);
  }

  if (
    !("version" in body) ||
    typeof body.version !== "object" ||
    body.version == null
  ) {
    console.error(`Version details not found: ${name}`);
    console.error(bodyJSON);
    process.exit(1);
  }

  const pkg = body.package;
  const { name: newName } = pkg;
  const { id: newId, semver, deployment_id: newDeploymentId } = body.version;
  if (
    typeof newId !== "string" ||
    typeof newName !== "string" ||
    typeof semver !== "string" ||
    typeof newDeploymentId !== "string"
  ) {
    console.error(
      `Version details are missing id, name, semver, and/or deployment_id fields: ${packageName}`
    );
    console.error(bodyJSON);
    process.exit(1);
  }

  return { id: newId, name: newName, deploymentId: newDeploymentId, semver };
}

async function logBadResponse(res: Response) {
  console.error(`Invalid response from ${res.url}`);
  console.error(`${res.status} ${res.statusText}`);
  console.error(await res.text());
  process.exit(1);
}

run();
