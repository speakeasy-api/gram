import type { CallOutcome } from "./model";

/** Fill for a call's decision wherever it is drawn. */
export const OUTCOME_FILL: Record<CallOutcome, string> = {
  permit: "bg-muted-foreground/30",
  clarify: "bg-warning-default",
  block: "bg-destructive-default",
  failed: "text-muted-foreground",
};

// Diagonal hatching marks an evaluator failure so it never reads as a block.
export const HATCH_STYLE = {
  backgroundImage:
    "repeating-linear-gradient(135deg, currentColor 0 1px, transparent 1px 5px)",
};

export const OUTCOME_BADGE: Record<
  CallOutcome,
  "neutral" | "warning" | "destructive"
> = {
  permit: "neutral",
  clarify: "warning",
  block: "destructive",
  failed: "neutral",
};

export const OUTCOME_TEXT: Record<CallOutcome, string> = {
  permit: "text-muted-foreground",
  clarify: "text-warning",
  block: "text-destructive",
  failed: "text-muted-foreground",
};

/** Hatched warning band marking that a secret is in the agent's context. */
export const SECRET_BAND = "border border-warning-default text-warning";
