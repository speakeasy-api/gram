import type { AdminAIProductDocument } from "@gram/admin-client/models/components/adminaiproductdocument";

import { badgeTone } from "@/lib/badgeTone";

const CATEGORY_LABELS: Record<string, string> = {
  harness: "Coding agent",
  assistant: "Assistant",
  local_model: "Local model",
};

// A product with no category is known only by its sign-in documents.
export function categoryLabel(category: string | undefined): string {
  if (!category) return "Sign-in only";
  return CATEGORY_LABELS[category] ?? category;
}

export type Verification = { label: string; tone: string; detail?: string };

export function verificationState(
  document: Pick<AdminAIProductDocument, "verifiedAt" | "verificationError">,
): Verification {
  if (document.verificationError) {
    return {
      label: "Check failed",
      tone: badgeTone.warning,
      detail: `Reason: ${document.verificationError}`,
    };
  }
  if (document.verifiedAt) {
    return {
      label: "Checked",
      tone: badgeTone.success,
      detail: `Last checked ${document.verifiedAt.toLocaleString()}`,
    };
  }
  return { label: "Not checked", tone: badgeTone.neutral };
}
