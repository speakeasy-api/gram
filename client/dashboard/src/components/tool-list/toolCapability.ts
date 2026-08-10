import type { BadgeVariant } from "@/components/ui/lib/types";

/** The MCP tool annotation hints used to classify a tool's capability. */
export interface ToolCapabilityAnnotations {
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
}

/**
 * A single read/write/destructive capability derived from a tool's annotation
 * hints — the coarse "what can this tool do" signal admins reason about when
 * granting access, distinct from the raw per-hint labels in `AnnotationBadges`.
 */
export type ToolCapability = "read" | "write" | "destructive";

export const CAPABILITY_META: Record<
  ToolCapability,
  { label: string; variant: BadgeVariant; tooltip: string }
> = {
  read: {
    label: "Read",
    variant: "neutral",
    tooltip: "Read-only — this tool doesn't modify its environment.",
  },
  write: {
    label: "Write",
    variant: "information",
    tooltip: "Write — this tool can modify its environment.",
  },
  destructive: {
    label: "Destructive",
    variant: "destructive",
    tooltip: "Destructive — this tool may perform destructive updates.",
  },
};

/**
 * Derives a single read/write/destructive capability from a tool's annotation
 * hints. Mirrors the server's disposition priority (read_only > destructive)
 * in `conv.DispositionFromAnnotations`: a read-only tool reads, an explicitly
 * destructive one is destructive, and any other tool the source asserts is not
 * read-only is a write. Returns null when the source made no assertion (all
 * hints unset), so callers can omit the chip rather than guess.
 */
export function toolCapability(
  annotations?: ToolCapabilityAnnotations | null,
): ToolCapability | null {
  if (!annotations) return null;
  const { readOnlyHint, destructiveHint } = annotations;
  if (readOnlyHint === true) return "read";
  if (destructiveHint === true) return "destructive";
  if (readOnlyHint === false) return "write";
  return null;
}
