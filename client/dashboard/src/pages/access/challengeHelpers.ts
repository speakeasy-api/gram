import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";

type Reason = AuthzChallenge["reason"];

export function getInitials(identifier: string): string {
  const name = identifier.split("@")[0] ?? identifier;
  return name.slice(0, 2).toUpperCase();
}

export function reasonLabel(reason: Reason): string {
  switch (reason) {
    case "grant_matched":
      return "Access granted — a matching role permission was found.";
    case "no_grants":
      return "No permissions configured for this identity.";
    case "scope_unsatisfied":
      return "The identity's roles don't include this permission.";
    case "invalid_check":
      return "The authorization check was malformed or invalid.";
    case "rbac_skipped_apikey":
      return "API keys bypass role checks — access was allowed directly.";
    case "dev_override":
      return "Access allowed via a development override.";
  }
}
