import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { cn } from "@/lib/utils";

/**
 * Vendor marks by aivendors registry id.
 *
 * Mapped explicitly rather than through agentProviderIconKind, whose substring
 * matching would give an organization's own "cursor-fork" target the Cursor
 * logo. In the table people read to decide what to block, a confident wrong
 * mark is worse than none, so anything unlisted gets the monogram.
 */
const ICON_SOURCE_BY_TARGET_ID: Record<string, string> = {
  "claude-code": "claude",
  claude: "claude",
  // ChatGPT and Codex share OpenAI's mark; AgentProviderIcon resolves both
  // through its codex case, which is the OpenAI monoblossom.
  "chatgpt-classic": "chatgpt",
  codex: "codex",
  cursor: "cursor",
  opencode: "opencode",
  openclaw: "openclaw",
  "gemini-cli": "gemini",
  windsurf: "windsurf",
  ollama: "ollama",
  lmstudio: "lmstudio",
  "hermes-agent": "hermes",
  cline: "cline",
  warp: "warp",
  trae: "trae",
  vllm: "vllm",
  // Devin and Windsurf are one vendor's two shapes: Devin Desktop is the
  // rebranded Windsurf, so both carry Cognition's mark.
  devin: "devin",
};

/**
 * AIToolMonogram is what a tool with no vendor mark gets: the first character
 * of its name in a neutral tile.
 *
 * Deliberately not AgentProviderIcon's globe fallback. On the Models tab every
 * row would be the same globe, which reads as a broken image rather than as
 * "no logo"; a monogram still tells rows apart and never claims a vendor.
 */
function AIToolMonogram({
  displayName,
  className,
}: {
  displayName: string;
  className?: string;
}): JSX.Element {
  const initial = Array.from(displayName.trim())[0]?.toUpperCase() ?? "?";
  return (
    <span
      aria-hidden
      className={cn(
        "bg-muted text-muted-foreground flex items-center justify-center rounded-sm text-[10px] font-medium",
        className,
      )}
    >
      {initial}
    </span>
  );
}

export function AIToolIcon({
  targetId,
  displayName,
  className,
}: {
  targetId: string;
  displayName: string;
  className?: string;
}): JSX.Element {
  const source = ICON_SOURCE_BY_TARGET_ID[targetId];
  if (source === undefined) {
    return <AIToolMonogram displayName={displayName} className={className} />;
  }
  return <AgentProviderIcon source={source} className={className} />;
}
