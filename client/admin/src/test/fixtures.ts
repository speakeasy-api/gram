// Records with nothing interesting on them. A test overrides the one field it
// is about, so a field nobody set cannot decide an assertion.
//
// Invented names throughout. This repository is public, so no fixture ever
// carries a real organization, project or person.

import type {
  AdminOrganization,
  AdminOrganizationMember,
  AdminProject,
} from "@/lib/gramAdminApi";

export function anOrganization(
  overrides: Partial<AdminOrganization> = {},
): AdminOrganization {
  return {
    id: "org_1",
    name: "Test Org",
    slug: "test-org",
    account_type: "free",
    whitelisted: false,
    member_count: 3,
    created_at: "2026-01-15T00:00:00Z",
    updated_at: "2026-01-15T00:00:00Z",
    ...overrides,
  };
}

export function aProject(overrides: Partial<AdminProject> = {}): AdminProject {
  return {
    id: "proj_1",
    name: "First Project",
    slug: "first-project",
    created_at: "2026-01-15T00:00:00Z",
    updated_at: "2026-01-15T00:00:00Z",
    ...overrides,
  };
}

export function aMember(
  overrides: Partial<AdminOrganizationMember> = {},
): AdminOrganizationMember {
  return {
    id: "user_1",
    email: "member@example.test",
    display_name: "Placeholder Member",
    last_login: "2026-01-05T00:00:00Z",
    created_at: "2026-01-06T00:00:00Z",
    updated_at: "2026-01-06T00:00:00Z",
    ...overrides,
  };
}
