import speakeasyIcon from "@/assets/speakeasy-icon.svg";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import type { EventLogEntryKind } from "@gram/client/models/components/eventlogentry.js";

/** Mono uppercase tag for a signal kind — `log` or `span`. */
export function EventKindBadge({
  kind,
}: {
  kind: EventLogEntryKind;
}): JSX.Element {
  return (
    <span className="border-border text-muted-foreground border px-1.5 py-0.5 font-mono text-[10px] tracking-wide uppercase">
      {kind}
    </span>
  );
}

// Sources canonicalized from Gram's own services (e.g. gram-server) carry the
// Speakeasy mark; everything else routes through the shared agent-provider
// icon set (claude-code, litellm, ...), which falls back to a globe. The
// match is exact-or-hyphenated so unrelated services that merely start with
// "gram" don't pick up the mark.
function isGramSource(source: string): boolean {
  const canonical = source.trim().toLowerCase();
  return canonical === "gram" || canonical.startsWith("gram-");
}

/** Logo for a canonicalized event source (resource service name). */
export function EventSourceIcon({
  source,
  className,
}: {
  source: string;
  className?: string;
}): JSX.Element {
  if (isGramSource(source)) {
    return <img src={speakeasyIcon} alt="" className={className} />;
  }
  return <AgentProviderIcon source={source} className={className} />;
}
