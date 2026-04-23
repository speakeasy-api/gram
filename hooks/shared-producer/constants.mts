export const RESOLUTION_STATUSES = [
  "resolved",
  "unresolved_name_only",
  "invalid_skill_root",
  "skipped_by_author",
  "producer_unavailable",
  "capture_skipped_size_limit",
  "capture_skipped_file_limit",
  "capture_skipped_policy",
  "capture_skipped_missing_credentials",
  "capture_upload_failed",
] as const;

export type ResolutionStatus = (typeof RESOLUTION_STATUSES)[number];

export const DEFAULT_RESOLUTION_STATUS: ResolutionStatus =
  "unresolved_name_only";

export const SUPPORTED_AGENTS = ["claude", "cursor"] as const;
export type SupportedAgent = (typeof SUPPORTED_AGENTS)[number];

export interface CaptureLimits {
  maxUncompressedBytes: number;
  maxCompressedBytes: number;
  maxFileCount: number;
  maxIndividualFileBytes: number;
}

export const CAPTURE_LIMITS: CaptureLimits = Object.freeze({
  maxUncompressedBytes: 25 * 1024 * 1024,
  maxCompressedBytes: 10 * 1024 * 1024,
  maxFileCount: 1000,
  maxIndividualFileBytes: 5 * 1024 * 1024,
});

export const BUILTIN_IGNORE_GLOBS = [
  ".git/**",
  ".hg/**",
  ".svn/**",
  ".DS_Store",
  "Thumbs.db",
  "._*",
  "*~",
  "*.swp",
  "*.swo",
  ".idea/**",
  ".vscode/**",
  "node_modules/**",
  "dist/**",
  "build/**",
  "coverage/**",
  ".cache/**",
  "__pycache__/**",
  ".venv/**",
  "venv/**",
  "*.log",
  "*.tmp",
  "*.temp",
] as const;

export type SkillScope = "project" | "user";
export type DiscoveryRootName =
  | "project_agents"
  | "project_claude"
  | "user_agents"
  | "user_claude"
  | "project_cursor"
  | "user_cursor";

export interface DiscoveryRootDefinition {
  discoveryRoot: DiscoveryRootName;
  scope: SkillScope;
  segments: readonly [string, string];
}

const CLAUDE_DISCOVERY_ROOTS = [
  {
    discoveryRoot: "project_agents",
    scope: "project",
    segments: [".agents", "skills"],
  },
  {
    discoveryRoot: "project_claude",
    scope: "project",
    segments: [".claude", "skills"],
  },
  {
    discoveryRoot: "user_agents",
    scope: "user",
    segments: [".agents", "skills"],
  },
  {
    discoveryRoot: "user_claude",
    scope: "user",
    segments: [".claude", "skills"],
  },
] as const satisfies readonly DiscoveryRootDefinition[];

const CURSOR_DISCOVERY_ROOTS = [
  {
    discoveryRoot: "project_agents",
    scope: "project",
    segments: [".agents", "skills"],
  },
  {
    discoveryRoot: "project_cursor",
    scope: "project",
    segments: [".cursor", "skills"],
  },
  {
    discoveryRoot: "user_agents",
    scope: "user",
    segments: [".agents", "skills"],
  },
  {
    discoveryRoot: "user_cursor",
    scope: "user",
    segments: [".cursor", "skills"],
  },
] as const satisfies readonly DiscoveryRootDefinition[];

export const DISCOVERY_ROOTS_BY_AGENT: Readonly<
  Record<SupportedAgent, readonly DiscoveryRootDefinition[]>
> = Object.freeze({
  claude: CLAUDE_DISCOVERY_ROOTS,
  cursor: CURSOR_DISCOVERY_ROOTS,
});
