import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { UserSessionIssuerFieldMismatch } from "@gram/client/models/components/usersessionissuerfieldmismatch.js";

/**
 * How the authorization server's MCP servers sign clients in. A shared one
 * this deployment does not serve falls back to one per MCP server, so it says
 * so rather than claiming to be shared.
 */
export function authorizationServerModeLabel(
  issuer: Pick<UserSessionIssuer, "authorizationServerMode" | "issuerUrl">,
): string {
  if (issuer.authorizationServerMode !== "shared") return "One per MCP server";
  return issuer.issuerUrl ? "Shared" : "Shared, unavailable";
}

/** Why a shared authorization server is not served, in plain words. */
export function unavailableReason(
  issuer: Pick<UserSessionIssuer, "authorizationServerUnavailableReason">,
): string {
  const reason = issuer.authorizationServerUnavailableReason;
  if (reason === "pinned_issuer_url_invalid") {
    return "Its fixed issuer URL doesn't belong to it";
  }
  if (reason === "host_not_served") {
    return "This deployment doesn't serve its address";
  }
  return "It isn't available right now";
}

/**
 * The authorization servers another one can be consolidated into: any other
 * organization-wide one, or, for a project's own, one in the same project.
 * The server's preflight still has the final say.
 */
export function consolidationTargets(
  source: UserSessionIssuer,
  pool: UserSessionIssuer[],
): UserSessionIssuer[] {
  return pool.filter(
    (candidate) =>
      candidate.id !== source.id &&
      (candidate.projectId === "" ||
        (source.projectId !== "" && candidate.projectId === source.projectId)),
  );
}

const sessionDuration = (hours: string): string => {
  const value = Number(hours);
  if (!Number.isFinite(value)) return hours;
  if (value % 24 === 0) {
    const days = value / 24;
    return `${days} ${days === 1 ? "day" : "days"}`;
  }
  return `${value} ${value === 1 ? "hour" : "hours"}`;
};

const configured = (value: string): string =>
  value === "" ? "None" : "Configured";

/**
 * What each setting a consolidation preflight compares is called, and how its
 * values read. Fields the server adds later fall back to their raw name and
 * values rather than disappearing from the warning.
 */
const MISMATCH_FIELDS: Record<
  string,
  { label: string; value: (raw: string) => string }
> = {
  session_duration_hours: { label: "Session length", value: sessionDuration },
  client_id_metadata_admission_mode: {
    label: "Client admission",
    value: (raw) =>
      ({
        disabled: "No self-describing clients",
        presets: "Approved clients only",
        open: "Any client",
        reporting: "Any client",
      })[raw] ?? raw,
  },
  authn_challenge_mode: {
    label: "Sign-in flow",
    value: (raw) =>
      ({ chain: "Chained", interactive: "Interactive" })[raw] ?? raw,
  },
  trusted_remote_session_issuer_id: {
    label: "Trusted identity provider",
    value: configured,
  },
  trusted_remote_session_client_id: {
    label: "Trusted identity provider client",
    value: configured,
  },
  authorization_server_mode: {
    label: "Mode",
    value: (raw) =>
      raw === "shared"
        ? "Shared"
        : raw === "endpoint"
          ? "One per MCP server"
          : raw,
  },
  use_authentication_host: {
    label: "Authentication host",
    value: (raw) => (raw === "true" ? "Used" : "Not used"),
  },
  issuer_url: {
    label: "Issuer URL",
    value: (raw) => (raw === "" ? "None, one per MCP server" : raw),
  },
};

/** Settings worded only by whether they are configured, by what they name. */
const CONFIGURED_ONLY_FIELDS: Record<string, string> = {
  trusted_remote_session_issuer_id: "provider",
  trusted_remote_session_client_id: "client",
};

/** How one of an authorization server's settings reads on the page. */
export function settingValue(field: string, raw: string): string {
  return MISMATCH_FIELDS[field]?.value(raw) ?? raw;
}

/** One consolidation warning, in the words the page uses. */
export function describeMismatch(mismatch: UserSessionIssuerFieldMismatch): {
  label: string;
  from: string;
  to: string;
} {
  const field = MISMATCH_FIELDS[mismatch.field];
  if (!field) {
    return {
      label: mismatch.field,
      from: mismatch.sourceValue || "None",
      to: mismatch.targetValue || "None",
    };
  }
  let from = field.value(mismatch.sourceValue);
  let to = field.value(mismatch.targetValue);
  // Two different identity providers, or clients, read the same once reduced
  // to whether one is configured; say they differ instead.
  const noun = CONFIGURED_ONLY_FIELDS[mismatch.field];
  if (from === to && noun) {
    from = `One ${noun}`;
    to = "A different one";
  }
  return { label: field.label, from, to };
}

/** Whether consolidating moves clients to a different issuer URL. */
export function changesIssuerUrl(
  warnings: UserSessionIssuerFieldMismatch[],
): boolean {
  return warnings.some((warning) => warning.field === "issuer_url");
}
