import type { ShadowMCPAccessRule } from "@gram/client/models/components/shadowmcpaccessrule.js";
import type { ShadowMCPApprovalRequest } from "@gram/client/models/components/shadowmcpapprovalrequest.js";

export type ShadowMCPMatchBreadth = "full_url" | "url_host";

export type ShadowMCPDisposition = "allowed" | "denied";

export type ShadowMCPAccessScope = "organization" | "project";

export function normalizeRuleMatchBreadth(
  matchBreadth: string | undefined,
): ShadowMCPMatchBreadth {
  switch (matchBreadth) {
    case "full_url":
      return "full_url";
    case "url_host":
      return "url_host";
    case undefined:
      return "full_url";
    default:
      return "full_url";
  }
}

export function getMatchBreadthLabel(matchBreadth: string | undefined): string {
  switch (normalizeRuleMatchBreadth(matchBreadth)) {
    case "full_url":
      return "Full URL";
    case "url_host":
      return "URL host";
  }
}

export function getDispositionLabel(disposition: ShadowMCPDisposition): string {
  return disposition === "allowed" ? "Allowed" : "Denied";
}

export function getAccessScopeLabel(accessScope: ShadowMCPAccessScope): string {
  return accessScope === "organization" ? "Organization" : "Project";
}

export function getResourceTypeLabel(resourceType: string | undefined): string {
  switch (resourceType) {
    case "shadow_mcp":
      return "Shadow MCP";
    case undefined:
      return "Unknown";
    default:
      return resourceType;
  }
}

export function getRequestStatusLabel(
  status: ShadowMCPApprovalRequest["status"],
): string {
  switch (status) {
    case "requested":
      return "Requested";
    case "approved":
      return "Approved";
    case "denied":
      return "Denied";
  }
}

export function formatShortDate(value: Date | string | undefined): string {
  if (!value) return "Never";

  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(value));
}

export function getRequestDisplayName(
  request: ShadowMCPApprovalRequest,
): string {
  return (
    request.observedName ??
    request.observedServerIdentity ??
    request.observedUrlHost ??
    request.observedFullUrl ??
    "Unknown server"
  );
}

export function getRuleDisplayName(rule: ShadowMCPAccessRule): string {
  return (
    rule.displayName ||
    rule.observedServerIdentity ||
    rule.matchValue ||
    "Unknown server"
  );
}

export function getRequestServerDetail(
  request: ShadowMCPApprovalRequest,
): string | undefined {
  if (request.observedFullUrl) return request.observedFullUrl;
  if (request.observedUrlHost) return request.observedUrlHost;
  if (request.observedServerIdentity) {
    return `Server identity: ${request.observedServerIdentity}`;
  }
  return undefined;
}

export function getRuleServerDetail(
  rule: ShadowMCPAccessRule,
): string | undefined {
  if (rule.matchValue) {
    return `${getMatchBreadthLabel(rule.matchBreadth)}: ${rule.matchValue}`;
  }
  if (rule.observedFullUrl) return rule.observedFullUrl;
  if (rule.observedUrlHost) return rule.observedUrlHost;
  if (rule.observedServerIdentity) {
    return `Server identity: ${rule.observedServerIdentity}`;
  }
  return undefined;
}

export function getRequesterLabel(request: ShadowMCPApprovalRequest): string {
  return (
    request.requesterDisplayName ??
    request.requesterEmail ??
    request.requesterUserId ??
    "Unknown user"
  );
}

export function getRequesterDetail(
  request: ShadowMCPApprovalRequest,
): string | undefined {
  if (request.requesterDisplayName && request.requesterEmail) {
    return request.requesterEmail;
  }

  return request.requesterUserId;
}

export function getDefaultMatchBreadth(
  source: Pick<
    ShadowMCPApprovalRequest | ShadowMCPAccessRule,
    "observedFullUrl" | "observedUrlHost" | "observedServerIdentity"
  >,
): ShadowMCPMatchBreadth {
  if (source.observedFullUrl) return "full_url";
  if (source.observedUrlHost) return "url_host";
  return "full_url";
}

export function getMatchValue(
  source: Pick<
    ShadowMCPApprovalRequest | ShadowMCPAccessRule,
    "observedFullUrl" | "observedUrlHost" | "observedServerIdentity"
  >,
  matchBreadth: ShadowMCPMatchBreadth,
): string {
  switch (matchBreadth) {
    case "full_url":
      return source.observedFullUrl ?? "";
    case "url_host":
      return source.observedUrlHost ?? "";
  }
}
