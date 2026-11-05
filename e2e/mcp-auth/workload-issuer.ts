import {
  generateKeyPairSync,
  randomUUID,
  sign,
  X509Certificate,
} from "node:crypto";
import { lookup } from "node:dns/promises";
import { isIP } from "node:net";
import { readFile } from "node:fs/promises";
import type { IncomingMessage, ServerResponse } from "node:http";
import { resolve } from "node:path";
import type { Discovery } from "./client.ts";
import type { AuthScenario } from "./fixtures.ts";
import { acquireHost, hostOrigin } from "./scenario-host.ts";
import { createScenario, type ScenarioRequest } from "./scenario-store.ts";

/** Ephemeral protocol identity: the TLS private key is never a JWT signing key. */
export function workloadSigner() {
  const { privateKey, publicKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
  });
  const kid = randomUUID();
  const jwk = {
    ...publicKey.export({ format: "jwk" }),
    kid,
    alg: "RS256",
    use: "sig",
  };
  return {
    jwk,
    issue(issuer: string, subject: string, audience: string) {
      const now = Math.floor(Date.now() / 1000);
      const encode = (value: unknown) =>
        Buffer.from(JSON.stringify(value)).toString("base64url");
      const payload = `${encode({ alg: "RS256", typ: "JWT", kid })}.${encode({ iss: issuer, sub: subject, aud: audience, iat: now, exp: now + 120, jti: randomUUID() })}`;
      return `${payload}.${sign("RSA-SHA256", Buffer.from(payload), privateKey).toString("base64url")}`;
    },
  };
}

/** Fail closed before opening the loopback listener or advertising an issuer. */
export function validateWorkloadHostname(hostname: string) {
  if (
    isIP(hostname) !== 0 ||
    hostname.length > 253 ||
    !hostname.includes(".") ||
    !hostname
      .split(".")
      .every((label) => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))
  ) {
    throw new Error(
      "ENV_WORKLOAD_ISSUER: MCP_AUTH_WORKLOAD_HOST must be an FQDN",
    );
  }
}

export function validateWorkloadAddresses(
  addresses: readonly { address: string }[],
) {
  if (
    addresses.length === 0 ||
    addresses.some(({ address }) => address !== "127.0.0.1")
  ) {
    throw new Error(
      "ENV_WORKLOAD_ISSUER: MCP_AUTH_WORKLOAD_HOST must resolve exclusively to 127.0.0.1 for both this runner and the native local-mode server",
    );
  }
}

/**
 * Dedicated TLS material, independent of dashboard/server certificates.
 * Default DNS depends on sslip.io; every startup checks exclusively IPv4 loopback.
 * .localhost can also resolve ::1, which this IPv4-only listener cannot serve.
 * With an EXISTING trusted mkcert CA (never install a CA here), provision with:
 * mkcert -cert-file local/ssl/certs/workload.pem -key-file local/ssl/keys/workload-key.pem 127.0.0.1.sslip.io
 * Node must also trust that CA, e.g. NODE_EXTRA_CA_CERTS="$(mkcert -CAROOT)/rootCA.pem".
 */
export async function loadWorkloadTLS() {
  const root = resolve(import.meta.dirname, "../..");
  const hostname = process.env.MCP_AUTH_WORKLOAD_HOST || "127.0.0.1.sslip.io";
  validateWorkloadHostname(hostname);
  validateWorkloadAddresses(
    await lookup(hostname, { all: true }).catch(() => []),
  );
  const [cert, key] = await Promise.all([
    readFile(
      process.env.MCP_AUTH_WORKLOAD_CERT_FILE ||
        resolve(root, "local/ssl/certs/workload.pem"),
    ),
    readFile(
      process.env.MCP_AUTH_WORKLOAD_KEY_FILE ||
        resolve(root, "local/ssl/keys/workload-key.pem"),
    ),
  ]);
  const certificate = new X509Certificate(cert);
  if (
    !certificate.checkHost(hostname) ||
    Date.now() < Date.parse(certificate.validFrom) ||
    Date.now() > Date.parse(certificate.validTo)
  ) {
    throw new Error(
      "ENV_WORKLOAD_ISSUER: MCP_AUTH_WORKLOAD_HOST must be covered by a current MCP_AUTH_WORKLOAD_CERT_FILE (dedicated trusted TLS certificate)",
    );
  }
  return { hostname, cert, key };
}

export function handleWorkloadRequest(
  request: ScenarioRequest,
  req: IncomingMessage,
  res: ServerResponse,
) {
  res.setHeader("Content-Type", "application/json");
  if (req.method === "GET" && request.path === "/jwks") {
    request.files.journal<true>("jwks-reads").push(true);
    res.end(
      JSON.stringify({
        keys: [request.files.map<unknown>("signer").get("jwk")],
      }),
    );
  } else if (
    req.method === "GET" &&
    request.path === "/.well-known/openid-configuration"
  ) {
    res.end(
      JSON.stringify({
        issuer: request.origin,
        jwks_uri: `${request.origin}/jwks`,
        id_token_signing_alg_values_supported: ["RS256"],
      }),
    );
  } else {
    res.writeHead(404).end("{}");
  }
}

export async function startWorkloadIssuer() {
  const host = await acquireHost();
  const { id, files } = createScenario(host.runDir, "workload");
  const origin = `${hostOrigin(host.info.workload)}/s/${id}`;
  const signer = workloadSigner();
  files.map<unknown>("signer").set("jwk", signer.jwk);
  const jwksReads = files.journal<true>("jwks-reads");
  const exchanges: {
    status: number;
    resourceScoped: boolean;
    errorCode?: string;
  }[] = [];
  return {
    origin,
    jwksURL: `${origin}/jwks`,
    subject: "fixture:workload:runner",
    exchanges,
    get jwksReads() {
      return jwksReads.list().length;
    },
    issue: (audience: string) =>
      signer.issue(origin, "fixture:workload:runner", audience),
    close: () => host.release(),
  };
}

/** Creates the assignment and trust policy exclusively through the real agent wizard. */
export async function createWorkloadAgent(
  auth: AuthScenario,
  issuer: Awaited<ReturnType<typeof startWorkloadIssuer>>,
  server: { name: string; serverURL: string },
  scope: "project" | "organization" = "project",
) {
  const { expect } = await import("./fixtures.ts");
  const { page, dashboard } = auth;
  const name = `fixture-workload-${randomUUID().slice(0, 8)}`;
  await page.goto(`${server.serverURL}/settings`);
  const remoteURL = page.getByLabel("Remote URL", { exact: true });
  await expect(remoteURL).toHaveCount(1);
  await expect(remoteURL).toHaveValue(/^https?:\/\//);
  const upstream = new URL(await remoteURL.inputValue());
  const resourceName = `${upstream.host}${upstream.pathname}`;
  await page.goto(`${dashboard.projectURL}/identities/agents/new`);
  const field = page
    .locator(".gram-elements")
    .getByRole("textbox", { name: "Name", exact: true });
  await expect(field).toBeEditable();
  await field.fill(name);
  await field.press("Tab");
  const next = page.getByRole("button", { name: "Continue", exact: true });
  await next.click();
  await expect(
    page.getByRole("heading", { name: "MCP Selection", exact: true }),
  ).toBeVisible();
  const serverChoice = page
    .getByRole("checkbox", { name: server.name, exact: true })
    .or(page.getByRole("checkbox", { name: resourceName, exact: true }));
  await expect(
    serverChoice,
    "WORKLOAD_SELECTION_IDENTITY: exactly one server must match its name or persisted remote host/path",
  ).toHaveCount(1);
  await expect(
    serverChoice,
    "WORKLOAD_SELECTION_PREREQUISITE: owner needs server scope and a selectable connection",
  ).toBeEnabled();
  await serverChoice.check();
  await next.click();
  await expect(
    page.getByRole("heading", { name: "MCP sign-in", exact: true }),
  ).toBeVisible();
  await expect(
    next,
    "WORKLOAD_SIGN_IN_PREREQUISITE: owner connection and mcp:auth delegation must be ready",
  ).toBeEnabled();
  await next.click();
  await expect(
    page.getByRole("heading", { name: "Agent sign-in", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", {
      name: "Advanced: trust another platform's tokens",
      exact: true,
    })
    .click();
  const custom = page.getByRole("radio", { name: /^Custom OIDC issuer/ });
  await custom.focus();
  await custom.press("Space");
  await expect(custom).toBeChecked();
  await next.click();
  await expect(
    page.getByRole("heading", { name: "Identity provider", exact: true }),
  ).toBeVisible();
  const createIssuer = page.getByRole("button", {
    name: "Create new workload issuer",
    exact: true,
  });
  await expect(
    createIssuer,
    "WORKLOAD_ISSUER_PREREQUISITE: creating issuer trust requires workload:write",
  ).toBeEnabled();
  await createIssuer.click();
  const dialog = page.getByRole("dialog", {
    name: "Create workload issuer",
    exact: true,
  });
  await dialog.getByLabel("Name", { exact: true }).fill(name);
  await dialog.getByLabel("Issuer URL", { exact: true }).fill(issuer.origin);
  await dialog.getByLabel("JWKS URL", { exact: true }).fill(issuer.jwksURL);
  await dialog.getByLabel("Available to", { exact: true }).selectOption(scope);
  await dialog
    .getByRole("button", { name: "Create workload issuer", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  const selectedTrust = page.locator("dl").filter({
    has: page.getByText("External JWT issuer", { exact: true }),
  });
  await expect(selectedTrust).toHaveCount(1);
  await expect(selectedTrust.locator("dd")).toHaveText([
    issuer.origin,
    issuer.jwksURL,
  ]);
  await expect(next).toBeEnabled();
  await next.click();
  await page.getByLabel("Subject", { exact: true }).fill(issuer.subject);
  await page
    .getByRole("button", { name: "Connect platform", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Point it at Speakeasy", exact: true }),
  ).toBeVisible();
  await next.click();
  await expect(
    page.getByRole("heading", { name: "Check it worked", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  return name;
}

/** Only protocol grants, never management API provisioning. No token payload is logged. */
export async function exchangeWorkload(
  issuer: Awaited<ReturnType<typeof startWorkloadIssuer>>,
  metadata: Discovery,
  resourceScoped = false,
) {
  const body = new URLSearchParams({
    grant_type: "urn:ietf:params:oauth:grant-type:jwt-bearer",
    assertion: issuer.issue(metadata.authorizationServer),
  });
  if (resourceScoped) body.set("resource", metadata.resource);
  const response = await fetch(metadata.tokenEndpoint, {
    method: "POST",
    body,
    redirect: "error",
    signal: AbortSignal.timeout(15_000),
  });
  const value = (await response.json()) as {
    access_token?: string;
    error?: string;
    refresh_token?: string;
  };
  const errorCode =
    value.error === undefined
      ? undefined
      : typeof value.error === "string" &&
          value.error.length <= 64 &&
          /^[_a-z]+$/.test(value.error)
        ? value.error
        : "unrecognized_error";
  issuer.exchanges.push({ status: response.status, resourceScoped, errorCode });
  return { status: response.status, ...value, errorCode };
}
