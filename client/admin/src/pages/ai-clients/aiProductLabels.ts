import type { AdminAIProductDocument } from "@gram/admin-client/models/components/adminaiproductdocument";

import { badgeTone } from "@/lib/badgeTone";

const CATEGORY_LABELS: Record<string, string> = {
  harness: "Coding agent",
  assistant: "Assistant",
  local_model: "Local model",
};

export function categoryLabel(category: string | undefined): string {
  if (!category) return "None (sign-in documents only)";
  return CATEGORY_LABELS[category] ?? category;
}

// Rows with no author came from the built-in list through the startup seed.
export const BUILT_IN_SOURCE = "Added by the built-in list";

export function staffLabel(subject: string): string {
  return `Staff ID ${subject}`;
}

export function sourceLabel(createdBy: string | undefined): string {
  return createdBy ? `Added by ${staffLabel(createdBy)}` : BUILT_IN_SOURCE;
}

// The reasons the CIMD validator records when it rejects a document.
const CHECK_FAILURES: Record<string, string> = {
  client_id_too_long: "Client ID is too long",
  client_id_scheme: "Client ID is not an https address",
  client_id_fragment: "Client ID has a fragment",
  client_id_unparseable: "Client ID is not a valid address",
  client_id_userinfo: "Client ID contains a user name or password",
  client_id_missing_host: "Client ID has no host",
  client_id_missing_path: "Client ID has no path",
  client_id_dot_segments: "Client ID has . or .. in its path",
  client_id_mismatch: "Document names a different client ID",
  missing_client_name: "Document has no client name",
  client_name_too_long: "Client name is too long",
  invalid_auth_method: "Sign-in method is not allowed",
  contains_secret: "Document contains a client secret",
  jwks_invalid: "Key set is not valid",
  jwks_private_key: "Key set contains a private key",
  jwks_symmetric_key: "Key set contains a shared secret key",
  jwks_uri_invalid: "Key set address is not valid",
  jwks_no_signing_key: "Key set has no signing key",
  key_source_missing: "Document has no keys",
  key_source_ambiguous: "Document gives keys in two places",
  missing_redirect_uris: "Document has no redirect addresses",
  too_many_redirect_uris: "Document has too many redirect addresses",
  redirect_uri_too_long: "A redirect address is too long",
  redirect_uri_invalid: "A redirect address is not valid",
  redirect_uri_scheme: "A redirect address uses a scheme that is not allowed",
};

export function checkFailureLabel(code: string): string {
  const label = CHECK_FAILURES[code];
  return label ? `${label} (${code})` : `Check failed (${code})`;
}

export type Verification = { label: string; tone: string; detail?: string };

export function verificationState(
  document: Pick<AdminAIProductDocument, "verifiedAt" | "verificationError">,
): Verification {
  if (document.verificationError) {
    return {
      label: "Check failed",
      tone: badgeTone.warning,
      detail: checkFailureLabel(document.verificationError),
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
