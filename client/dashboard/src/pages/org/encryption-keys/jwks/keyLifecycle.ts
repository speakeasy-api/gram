import type { BadgeVariant } from "@/components/ui/lib/types";
import type {
  JSONWebKey,
  KeyState,
} from "@gram/client/models/components/jsonwebkey.js";

// The publish-before-sign lifecycle of a published key, as the server enforces
// it: pending → active (activate), active → retired (retire), retired → active
// (activate), and any state → revoked (revoke). Retire is refused for a pending
// key, which has never signed anything to wind down from.

export type KeyLifecycleAction = "activate" | "retire" | "revoke";

// availableKeyActions returns the transitions the server accepts from a state,
// in menu order. Revoked is terminal: the row is soft-deleted and only listed
// as history.
export function availableKeyActions(state: KeyState): KeyLifecycleAction[] {
  switch (state) {
    case "pending":
      return ["activate", "revoke"];
    case "active":
      return ["retire", "revoke"];
    case "retired":
      return ["activate", "revoke"];
    case "revoked":
      return [];
  }
}

export function keyStateLabel(state: KeyState): string {
  switch (state) {
    case "pending":
      return "Pending";
    case "active":
      return "Active";
    case "retired":
      return "Retired";
    case "revoked":
      return "Revoked";
  }
}

// keyStateDescription is the one-line meaning of a state for tooltips and
// empty-state copy.
export function keyStateDescription(state: KeyState): string {
  switch (state) {
    case "pending":
      return "Published so verifiers can cache it, but not signing yet.";
    case "active":
      return "New tokens are signed with this key.";
    case "retired":
      return "No longer signing; still published so outstanding tokens verify.";
    case "revoked":
      return "Withdrawn. Tokens signed with it no longer verify.";
  }
}

export function keyStateBadgeVariant(state: KeyState): BadgeVariant {
  switch (state) {
    case "pending":
      return "information";
    case "active":
      return "success";
    case "retired":
      return "neutral";
    case "revoked":
      return "destructive";
  }
}

// keyAlgorithm reads the algorithm the published JWK document advertises. The
// document is opaque to the SDK (`any`), so the read is defensive: a document
// without a string `alg` is rendered as unknown rather than crashing the row.
export function keyAlgorithm(key: JSONWebKey): string {
  const jwk: unknown = key.publicJwk;
  if (jwk == null || typeof jwk !== "object") return "Unknown";
  const alg = (jwk as Record<string, unknown>)["alg"];
  return typeof alg === "string" && alg !== "" ? alg : "Unknown";
}

export type KeyActionCopy = {
  title: string;
  description: string;
  confirmLabel: string;
  destructive: boolean;
  successMessage: string;
};

// keyActionCopy is the confirmation for each transition. Each one spells out
// what happens to tokens already signed, because that is the consequence an
// operator cannot see from the key row and cannot take back afterwards.
export function keyActionCopy(action: KeyLifecycleAction): KeyActionCopy {
  switch (action) {
    case "activate":
      return {
        title: "Activate key?",
        description:
          "New tokens will be signed with this key. If another key is active it is retired in the same step: it stops signing but stays published, so tokens it already signed keep verifying.",
        confirmLabel: "Activate key",
        destructive: false,
        successMessage: "Key activated",
      };
    case "retire":
      return {
        title: "Retire key?",
        description:
          "This key stops signing new tokens but stays published, so tokens it already signed keep verifying. The set has no active key until you activate another one, and nothing can be signed with it in the meantime.",
        confirmLabel: "Retire key",
        destructive: false,
        successMessage: "Key retired",
      };
    case "revoke":
      return {
        title: "Revoke key?",
        description:
          "This withdraws the key from the published set immediately. Every token signed with it stops verifying, which breaks authentication for anything still presenting one, and the same KMS key can never be published into this set again. If you only want to stop signing, retire the key instead.",
        confirmLabel: "Revoke key",
        destructive: true,
        successMessage: "Key revoked",
      };
  }
}
