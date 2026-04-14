export const RESOLUTION_STATUSES = Object.freeze([
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
]);

export const DEFAULT_RESOLUTION_STATUS = "unresolved_name_only";

export const SUPPORTED_AGENTS = Object.freeze(["claude", "cursor"]);

export const CAPTURE_LIMITS = Object.freeze({
  maxUncompressedBytes: 25 * 1024 * 1024,
  maxCompressedBytes: 10 * 1024 * 1024,
  maxFileCount: 1000,
  maxIndividualFileBytes: 5 * 1024 * 1024,
});

export const BUILTIN_IGNORE_GLOBS = Object.freeze([
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
]);

export const DISCOVERY_ROOTS_BY_AGENT = Object.freeze({
  claude: Object.freeze([
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
  ]),
  cursor: Object.freeze([
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
  ]),
});
