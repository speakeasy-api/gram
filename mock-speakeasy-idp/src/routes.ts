import { Hono } from "hono";
import { logger } from "hono/logger";
import { randomUUID } from "crypto";
import { authMiddleware } from "./middleware.js";
import {
  generateAuthCode,
  validateAuthCode,
  generateToken,
  validateToken,
  revokeToken,
} from "./store.js";
import { getDevUser, getDevOrganizations } from "./fixtures.js";
import type {
  SpeakeasyOrganization,
  TokenExchangeRequest,
  CreateOrgRequest,
} from "./types.js";

export const app = new Hono();

// Track additional orgs created via /register per user
const userAdditionalOrgs = new Map<string, SpeakeasyOrganization[]>();

// Log all requests
app.use("*", logger());

// Apply auth middleware to all provider routes
app.use("/v1/speakeasy_provider/*", authMiddleware);

// GET /v1/speakeasy_provider/login
// Auto-approves login for local dev - generates auth code and redirects
app.get("/v1/speakeasy_provider/login", (c) => {
  const returnUrl = c.req.query("return_url");
  const state = c.req.query("state");

  console.log("[login] return_url=%s state=%s", returnUrl, state);

  if (!returnUrl) {
    console.log("[login] REJECTED: missing return_url");
    return c.json({ error: "Missing return_url parameter" }, 400);
  }

  const devUser = getDevUser();
  const code = generateAuthCode(devUser.id);

  const redirectUrl = new URL(returnUrl);
  redirectUrl.searchParams.set("code", code);
  if (state) {
    redirectUrl.searchParams.set("state", state);
  }

  console.log(
    "[login] OK: generated code=%s, redirecting to %s",
    code,
    redirectUrl.toString(),
  );
  return c.redirect(redirectUrl.toString(), 302);
});

// POST /v1/speakeasy_provider/exchange
// Exchanges an auth code for an ID token
app.post("/v1/speakeasy_provider/exchange", async (c) => {
  const body = (await c.req.json()) as TokenExchangeRequest;
  console.log("[exchange] code=%s", body.code);

  if (!body.code) {
    console.log("[exchange] REJECTED: missing code");
    return c.json({ error: "Missing code in request body" }, 400);
  }

  const userId = validateAuthCode(body.code);
  if (!userId) {
    console.log("[exchange] REJECTED: invalid code");
    return c.json({ error: "Invalid or expired auth code" }, 400);
  }

  const token = generateToken(userId);
  console.log("[exchange] OK: userId=%s token=%s", userId, token);
  return c.json({ id_token: token });
});

// GET /v1/speakeasy_provider/validate
// Validates a token and returns user + organizations
app.get("/v1/speakeasy_provider/validate", (c) => {
  const token = c.req.header("speakeasy-auth-provider-id-token");
  console.log("[validate] token=%s", token);

  if (!token) {
    console.log("[validate] REJECTED: missing token header");
    return c.json({ error: "Missing token header" }, 401);
  }

  const userId = validateToken(token);
  if (!userId) {
    console.log("[validate] REJECTED: invalid token");
    return c.json({ error: "Invalid or expired token" }, 401);
  }

  const user = getDevUser();
  const orgs = getAllOrgsForUser(userId);
  console.log(
    "[validate] OK: userId=%s email=%s orgs=%d",
    userId,
    user.email,
    orgs.length,
  );
  return c.json({ user, organizations: orgs });
});

// POST /v1/speakeasy_provider/revoke
// Revokes a token
app.post("/v1/speakeasy_provider/revoke", (c) => {
  const token = c.req.header("speakeasy-auth-provider-id-token");
  console.log("[revoke] token=%s", token);

  if (token) {
    revokeToken(token);
  }

  console.log("[revoke] OK");
  return c.json({ ok: true }, 200);
});

// POST /v1/speakeasy_provider/register
// Creates a new organization for the authenticated user
app.post("/v1/speakeasy_provider/register", async (c) => {
  const token = c.req.header("speakeasy-auth-provider-id-token");
  console.log("[register] token=%s", token);

  if (!token) {
    console.log("[register] REJECTED: missing token header");
    return c.json({ error: "Missing token header" }, 401);
  }

  const userId = validateToken(token);
  if (!userId) {
    console.log("[register] REJECTED: invalid token");
    return c.json({ error: "Invalid or expired token" }, 401);
  }

  const body = (await c.req.json()) as CreateOrgRequest;
  console.log(
    "[register] org_name=%s account_type=%s",
    body.organization_name,
    body.account_type,
  );

  if (!body.organization_name) {
    console.log("[register] REJECTED: missing organization_name");
    return c.json({ error: "Missing organization_name" }, 400);
  }

  const now = new Date().toISOString();
  const slug = slugify(body.organization_name);

  const newOrg: SpeakeasyOrganization = {
    id: randomUUID(),
    name: body.organization_name,
    slug,
    created_at: now,
    updated_at: now,
    account_type: body.account_type || "free",
    sso_connection_id: null,
    user_workspaces_slugs: [slug],
  };

  // Store the additional org
  const existing = userAdditionalOrgs.get(userId) || [];
  existing.push(newOrg);
  userAdditionalOrgs.set(userId, existing);

  const user = getDevUser();
  const orgs = getAllOrgsForUser(userId);

  return c.json({ user, organizations: orgs });
});

function getAllOrgsForUser(userId: string): SpeakeasyOrganization[] {
  const baseOrgs = getDevOrganizations();
  const additional = userAdditionalOrgs.get(userId) || [];
  return [...baseOrgs, ...additional];
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
