import type { TypesToInclude } from "@gram/client/models/components";

export const DEFAULT_HOOK_TYPES: TypesToInclude[] = ["mcp", "skill"];
export const VALID_HOOK_TYPES: TypesToInclude[] = ["mcp", "local", "skill"];

// Account-type filter options, shared across the observe surfaces (filter bar,
// Employees, Agent costs). "Personal" surfaces personal-account usage; "Team" is
// the implied default (everything without a personal account), mirroring the
// at-a-glance badge.
export const ACCOUNT_TYPE_OPTIONS = [
  { value: "personal", label: "Personal" },
  { value: "team", label: "Team" },
];
