import type { AdminCustomDomain } from "@gram/admin-client/models/components/admincustomdomain";
import type { DnsRoutingMethod } from "@gram/admin-client/models/operations/adminlistcustomdomains";

import { badgeTone } from "@/lib/badgeTone";

type Tone = keyof typeof badgeTone;

export type Label = { label: string; tone: Tone };

// Every routing method the filter offers, in the order an operator works
// through them: adopted, then the outreach candidates, then the ones nobody
// can tell apart yet.
export const ROUTING_METHODS = [
  "per_domain_cname",
  "shared_cname",
  "a_record",
  "unknown",
] as const satisfies readonly DnsRoutingMethod[];

// Filter wording. `unknown` also matches domains the health check has not
// reached yet, so the option says both.
export const ROUTING_FILTER_LABELS: Record<DnsRoutingMethod, string> = {
  per_domain_cname: "Per-domain CNAME",
  shared_cname: "Shared CNAME",
  a_record: "A record / flattened",
  unknown: "Unknown or not checked",
};

// A record covers flattened CNAME, ALIAS and ANAME records too: each resolves
// to addresses, so an apex domain that adopted its target through flattening
// looks the same as one pinned to an address.
export const A_RECORD_CAVEAT =
  "A record also covers flattened CNAME, ALIAS and ANAME records, which resolve to addresses. An apex domain that adopted its target through flattening cannot be told apart from one pinned to an address.";

// The badge for how the latest health check saw the domain routed. Absent
// means no health check has run yet, which is a different fact from a check
// that ran and could not tell. The response field is a plain string so a
// method added later still renders, as it came.
export function routingMethodLabel(method: string | undefined): Label {
  switch (method) {
    case "per_domain_cname":
      return { label: "Per-domain CNAME", tone: "success" };
    case "shared_cname":
      return { label: "Shared CNAME", tone: "warning" };
    case "a_record":
      return { label: "A record / flattened", tone: "neutral" };
    case "unknown":
      return { label: "Unknown", tone: "neutral" };
    case undefined:
    case "":
      return { label: "Not checked", tone: "neutral" };
    default:
      return { label: method, tone: "neutral" };
  }
}

export function domainStatusLabel(
  domain: Pick<AdminCustomDomain, "verified" | "activated">,
): Label {
  if (domain.verified && domain.activated) {
    return { label: "Active", tone: "success" };
  }
  if (domain.verified) {
    return { label: "Verified, not activated", tone: "warning" };
  }
  if (domain.activated) {
    return { label: "Activated, not verified", tone: "warning" };
  }
  return { label: "Not verified", tone: "neutral" };
}

// The server sends one of unknown, healthy or unhealthy, or nothing before the
// first check. Anything else is drawn as it came rather than laundered into a
// known state.
export function healthLabel(status: string | undefined): Label {
  switch (status) {
    case "healthy":
      return { label: "Healthy", tone: "success" };
    case "unhealthy":
      return { label: "Unhealthy", tone: "destructive" };
    case "unknown":
      return { label: "Unknown", tone: "neutral" };
    case undefined:
    case "":
      return { label: "Not checked", tone: "neutral" };
    default:
      return { label: status, tone: "neutral" };
  }
}

// Why the designated target has no value, so a blank cell is not read as a
// domain with no target at all.
export function missingTargetReason(
  domain: Pick<AdminCustomDomain, "dnsTargetToken">,
): string {
  return domain.dnsTargetToken
    ? "No target suffix configured on this admin server"
    : "Token not assigned yet";
}

// Date and time in UTC, for the one field here that moves more than once a
// day. Dates alone read through `fmtDateShort`.
export function fmtDateTimeUtc(date: Date | undefined): string {
  if (!date || Number.isNaN(date.getTime())) return "-";
  return `${date.toLocaleString(undefined, {
    timeZone: "UTC",
    dateStyle: "medium",
    timeStyle: "short",
  })} UTC`;
}

// DNS observations describe visible configuration, not endpoint availability.
export function dnsConfigurationLabel(status: string | undefined): Label {
  switch (status) {
    case "matched":
      return { label: "Target matches", tone: "success" };
    case "wrong_target":
      return { label: "Different target", tone: "warning" };
    case "unconfirmed":
      return { label: "Target unconfirmed", tone: "neutral" };
    case "not_found":
      return { label: "Name not found", tone: "warning" };
    case "check_failed":
      return { label: "Check incomplete", tone: "neutral" };
    case "not_configured":
      return { label: "Not configured", tone: "neutral" };
    case undefined:
    default:
      return { label: "Not checked", tone: "neutral" };
  }
}
