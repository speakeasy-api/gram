import type {
  SpeakeasyUser,
  SpeakeasyOrganization,
  ValidateTokenResponse,
} from "./types.js";

function env(key: string, fallback: string): string {
  return process.env[key] || fallback;
}

function envBool(key: string, fallback: boolean): boolean {
  const val = process.env[key];
  if (val === undefined) return fallback;
  return val === "true" || val === "1";
}

function buildDevUser(): SpeakeasyUser {
  const now = "2024-01-01T00:00:00Z";
  return {
    id: env("MOCK_IDP_USER_ID", "dev-user-1"),
    email: env("MOCK_IDP_USER_EMAIL", "dev@example.com"),
    display_name: env("MOCK_IDP_USER_DISPLAY_NAME", "Dev User"),
    photo_url: process.env["MOCK_IDP_USER_PHOTO_URL"] || null,
    github_handle: process.env["MOCK_IDP_USER_GITHUB_HANDLE"] || null,
    admin: envBool("MOCK_IDP_USER_ADMIN", true),
    created_at: now,
    updated_at: now,
    whitelisted: envBool("MOCK_IDP_USER_WHITELISTED", true),
  };
}

function buildDevOrganization(): SpeakeasyOrganization {
  const now = "2024-01-01T00:00:00Z";
  const slug = env("MOCK_IDP_ORG_SLUG", "local-dev-org");
  return {
    id: env("MOCK_IDP_ORG_ID", "550e8400-e29b-41d4-a716-446655440000"),
    name: env("MOCK_IDP_ORG_NAME", "Local Dev Org"),
    slug,
    created_at: now,
    updated_at: now,
    account_type: env("MOCK_IDP_ORG_ACCOUNT_TYPE", "free"),
    sso_connection_id: null,
    user_workspaces_slugs: [slug],
  };
}

const devUser = buildDevUser();
const devOrganization = buildDevOrganization();

export function getDevUser(): SpeakeasyUser {
  return { ...devUser };
}

export function getDevOrganizations(): SpeakeasyOrganization[] {
  return [{ ...devOrganization }];
}

export function getValidateResponse(): ValidateTokenResponse {
  return {
    user: getDevUser(),
    organizations: getDevOrganizations(),
  };
}
