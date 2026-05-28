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
    default:
      return "full_url";
  }
}

export function getMatchBreadthLabel(matchBreadth: string | undefined) {
  switch (normalizeRuleMatchBreadth(matchBreadth)) {
    case "full_url":
      return "Full URL";
    case "url_host":
      return "URL host";
  }
}

export function getDispositionLabel(disposition: ShadowMCPDisposition) {
  return disposition === "allowed" ? "Allowed" : "Denied";
}

export function getAccessScopeLabel(accessScope: ShadowMCPAccessScope) {
  return accessScope === "organization" ? "Organization" : "Project";
}

export function getResourceTypeLabel(resourceType: string | undefined) {
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
) {
  switch (status) {
    case "requested":
      return "Requested";
    case "approved":
      return "Approved";
    case "denied":
      return "Denied";
  }
}

export function formatShortDate(value: Date | string | undefined) {
  if (!value) return "Never";

  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(value));
}

export function getRequestDisplayName(request: ShadowMCPApprovalRequest) {
  return (
    request.observedName ??
    request.observedServerIdentity ??
    request.observedUrlHost ??
    request.observedFullUrl ??
    "Unknown server"
  );
}

export function getRuleDisplayName(rule: ShadowMCPAccessRule) {
  return rule.displayName || rule.observedServerIdentity || rule.matchValue;
}

export function getRequestServerDetail(request: ShadowMCPApprovalRequest) {
  if (request.observedFullUrl) return request.observedFullUrl;
  if (request.observedUrlHost) return request.observedUrlHost;
  if (request.observedServerIdentity) {
    return `Server identity: ${request.observedServerIdentity}`;
  }
  return undefined;
}

export function getRuleServerDetail(rule: ShadowMCPAccessRule) {
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

export function getRequesterLabel(request: ShadowMCPApprovalRequest) {
  return (
    request.requesterDisplayName ??
    request.requesterEmail ??
    request.requesterUserId ??
    "Unknown user"
  );
}

export function getRequesterDetail(request: ShadowMCPApprovalRequest) {
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
) {
  switch (matchBreadth) {
    case "full_url":
      return source.observedFullUrl ?? "";
    case "url_host":
      return source.observedUrlHost ?? "";
  }
}
