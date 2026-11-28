import { formatPlatform } from "@/lib/formatPlatform";

/**
 * Format the agent surface stored on a tool call block. Every block is written
 * by a hook path, and Claude Chat Desktop has no hooks, so a bare "claude"
 * adapter slug on a block always came from Claude Code. The shared label map
 * keeps "claude" as Claude Chat Desktop for the Anthropic compliance import,
 * so blocks resolve the slug here instead.
 */
export function formatBlockProvider(provider: string): string {
  if (provider.trim().toLowerCase() === "claude") {
    return formatPlatform("claude-code");
  }
  return formatPlatform(provider);
}
