import { useRiskUnmaskResultMutation } from "@gram/client/react-query/riskUnmaskResult.js";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { useCallback, useState } from "react";

// Revealing a flagged secret exposes the raw value captured from agent/chat
// traffic, so it is gated behind the same `chat:read` scope that grants access
// to other members' session transcripts. hasScope short-circuits to true when
// RBAC is disabled, preserving existing behavior for non-RBAC orgs.
export const REVEAL_SCOPE: Scope = "chat:read";
export const REVEAL_DENIED_REASON =
  "You need the chat:read scope to reveal flagged values.";

// The server redacts an absent match to this exact sentinel (no sha segment,
// unlike a real fingerprint). A prompt-based policy finding records the judge's
// verdict rather than a span of the message, so it lands here: there is no
// event behind the reveal, and offering one opens an empty dialog.
const NO_MATCH_FINGERPRINT = "<redacted len=0>";

export function hasRevealableEvent(
  matchRedacted: string | undefined,
): matchRedacted is string {
  return Boolean(matchRedacted) && matchRedacted !== NO_MATCH_FINGERPRINT;
}

// The general shape the server redacts a match to: the byte length, plus the
// first 8 hex of sha256(match) when there was anything to hash. Matching it is
// how a caller tells a fingerprint apart from a value passed through verbatim
// — which only shadow_mcp does, and only for rows written since that carve-out
// landed. Older shadow_mcp rows still carry a fingerprint, so source alone
// can't decide whether the value on screen is readable.
const REDACTION_FINGERPRINT = /^<redacted len=\d+( sha=[0-9a-f]+)?>$/;

export function isRedactionFingerprint(
  matchRedacted: string | undefined,
): boolean {
  return (
    matchRedacted !== undefined && REDACTION_FINGERPRINT.test(matchRedacted)
  );
}

// useUnmaskedMatch backs a single revealable value — a MaskedMatch row, or the
// exact-value option in the exclusion picker. It calls risk.unmaskResult on
// reveal and caches the plaintext locally so re-toggling visibility (or a
// second "reveal all" pass) never re-fetches or re-audits an already-seen
// value. Each reveal is a real, audited server call — there is no client-side
// stand-in for the plaintext until this resolves.
export function useUnmaskedMatch(resultId: string): {
  value: string | null;
  isLoading: boolean;
  reveal: () => void;
} {
  const { mutate, isPending } = useRiskUnmaskResultMutation();
  const [value, setValue] = useState<string | null>(null);
  const reveal = useCallback(() => {
    if (value !== null || isPending) return;
    mutate(
      { request: { riskIDRequestBody: { id: resultId } } },
      { onSuccess: (res) => setValue(res.match) },
    );
  }, [mutate, resultId, value, isPending]);
  return { value, isLoading: isPending, reveal };
}
