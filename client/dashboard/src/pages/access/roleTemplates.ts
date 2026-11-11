/**
 * Starting points for a new role. A template only fills the editor: the role
 * keeps no link to it, and any change afterwards makes the role its own.
 */

import type { Role } from "@gram/client/models/components/role.js";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";

import { grantsFromRole } from "./roleGrantTransform";
import type { RoleGrant } from "./types";

export interface RoleTemplate {
  key: string;
  label: string;
  hint: string;
  /** Copied from one of the organization's built-in roles. */
  system: boolean;
  /**
   * The name a new role starts with. It replaces an empty name or one an
   * earlier template put there, never one the person typed.
   */
  name: string;
  /** Builds the grants afresh, so two uses never share rule ids. */
  grants: () => Record<string, RoleGrant>;
}

/** Presets the dashboard defines: each grants its permissions everywhere. */
const PRESETS: {
  key: string;
  label: string;
  hint: string;
  scopes: string[];
}[] = [
  {
    key: "mcp-operator",
    label: "MCP operator",
    hint: "Manage and use every server, read environments.",
    scopes: ["project:read", "environment:read", "mcp:write"],
  },
  {
    key: "security-engineer",
    label: "Security engineer",
    hint: "Read-only review across the organization, use every server.",
    scopes: [
      "org:read",
      "project:read",
      "environment:read",
      "skill:read",
      "chat:read",
      "log:read",
      "workload:read",
      "agent:read",
      "mcp:connect",
    ],
  },
  {
    key: "team-reviewer",
    label: "Team reviewer",
    hint: "Read agent sessions and tool logs. No servers.",
    scopes: ["org:read", "project:read", "chat:read", "log:read"],
  },
];

/** The built-in roles a template can copy, by the slug the server gives them. */
const SYSTEM_TEMPLATES = [
  {
    slug: "member",
    hint: "What every member of this organization gets today.",
  },
  {
    slug: "admin",
    hint: "Everything an organization admin can do today.",
  },
];

function everywhere(slugs: string[]): Record<string, RoleGrant> {
  const grants: Record<string, RoleGrant> = Object.create(null);
  for (const slug of slugs) {
    const scope = slug as Scope;
    grants[scope] = {
      scope,
      rules: [{ id: crypto.randomUUID(), effect: "allow", selectors: null }],
    };
  }
  return grants;
}

/**
 * The templates a new role can start from. Member and Admin copy what the
 * organization's built-in roles grant right now, so they are offered only
 * when those roles are at hand; a preset keeps only the permissions the
 * catalogue serves.
 */
export function roleTemplates(
  scopes: ScopeDefinition[],
  roles: Role[],
): RoleTemplate[] {
  const grantable = new Set<string>(
    scopes
      .filter((scope) => scope.visibility === "user_visible")
      .map((scope) => scope.slug),
  );
  const templates: RoleTemplate[] = [
    {
      key: "blank",
      label: "Blank",
      hint: "Nothing granted. Build it up.",
      system: false,
      name: "",
      grants: () => ({}),
    },
  ];
  // Without the catalogue a role's blocks cannot be told from its grants, so
  // a copy would turn them into grants: offer the copies only once it loads.
  for (const { slug, hint } of scopes.length > 0 ? SYSTEM_TEMPLATES : []) {
    const role = roles.find(
      (candidate) => candidate.isSystem && candidate.slug === slug,
    );
    if (!role) continue;
    templates.push({
      key: `system:${slug}`,
      label: role.name,
      hint,
      system: true,
      name: `${role.name} copy`,
      grants: () => grantsFromRole(role, scopes),
    });
  }
  for (const preset of PRESETS) {
    const slugs = preset.scopes.filter((slug) => grantable.has(slug));
    if (slugs.length === 0) continue;
    templates.push({
      key: preset.key,
      label: preset.label,
      hint: preset.hint,
      system: false,
      name: preset.label,
      grants: () => everywhere(slugs),
    });
  }
  return templates;
}
