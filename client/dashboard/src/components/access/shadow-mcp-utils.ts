import type { Role } from "@gram/client/models/components/role.js";
import type { ShadowMCPAccessRule } from "@gram/client/models/components/shadowmcpaccessrule.js";
import type { ShadowMCPApprovalRequest } from "@gram/client/models/components/shadowmcpapprovalrequest.js";

export type ShadowMCPMatchBreadth = "full_url" | "url_host" | "server_identity";

export type ShadowMCPDisposition = "allowed" | "denied";

export interface ShadowMCPRoleOption {
  id: string;
  name: string;
  description?: string;
  isSystem?: boolean;
}

export function roleOptionsFromRoles(roles: Role[]): ShadowMCPRoleOption[] {
  return roles.map((role) => ({
    id: role.id,
    name: role.name,
    description: role.description,
    isSystem: role.isSystem,
  }));
}

export function getMatchBreadthLabel(matchBreadth: ShadowMCPMatchBreadth) {
  switch (matchBreadth) {
    case "full_url":
      return "Full URL";
    case "url_host":
      return "URL host";
    case "server_identity":
      return "Server identity";
  }
}

export function getDispositionLabel(disposition: ShadowMCPDisposition) {
  return disposition === "allowed" ? "Allowed" : "Denied";
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
  return "server_identity";
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
    case "server_identity":
      return source.observedServerIdentity ?? "";
  }
}

export function roleNamesForIds(
  roleIds: string[],
  roles: ShadowMCPRoleOption[],
) {
  return roleIds.map((roleId) => {
    const role = roles.find((candidate) => candidate.id === roleId);
    return role?.name ?? roleId;
  });
}
