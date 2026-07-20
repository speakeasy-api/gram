export const PLATFORM_LOGOS: Record<string, string> = {
  claude: "/icons/platforms/claude.svg",
  "claude-cowork": "/icons/platforms/claude.svg",
  codex: "/icons/platforms/openai.svg",
  cursor: "/icons/platforms/cursor.svg",
  // TODO: replace with official opencode brand asset once one is published.
  opencode: "/icons/platforms/opencode.svg",
};

// Monochrome black logos that are invisible on a dark background — flip them in
// dark mode. The Claude logo is full-color, so it must NOT be inverted.
export const INVERT_LOGO_IN_DARK = new Set<string>([
  "codex",
  "cursor",
  "opencode",
]);
