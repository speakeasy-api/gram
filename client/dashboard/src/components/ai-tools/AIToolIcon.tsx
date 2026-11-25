import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { cn } from "@/lib/utils";
import { ICON_SOURCE_BY_TARGET_ID } from "./ai-tool-icon-sources";

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
  // Own keys only: a custom target id such as "constructor" would otherwise
  // hit an inherited Object property and hand a function to the icon.
  const source = Object.hasOwn(ICON_SOURCE_BY_TARGET_ID, targetId)
    ? ICON_SOURCE_BY_TARGET_ID[targetId]
    : undefined;
  if (source === undefined) {
    return <AIToolMonogram displayName={displayName} className={className} />;
  }
  return <AgentProviderIcon source={source} className={className} />;
}
