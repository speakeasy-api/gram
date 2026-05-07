export type ShadowMCPMatchBreadth = "full_url" | "url_host";

export type ShadowMCPRequestStatus = "requested" | "approved" | "denied";

export type ShadowMCPDecision = "allowed" | "denied";

export interface ShadowMCPEvidence {
  name: string;
  fullUrl: string;
  urlHost: string;
  normalizedIdentity: string;
}

export interface ShadowMCPRequester {
  name: string;
  email: string;
}

export interface ShadowMCPApprovalRequest {
  id: string;
  status: ShadowMCPRequestStatus;
  evidence: ShadowMCPEvidence;
  requester: ShadowMCPRequester;
  requestedAt: string;
  lastBlockedAt: string;
  blockedCount: number;
  projectName: string;
  policyName: string;
  toolCall: string;
  notes?: string;
}

export interface ShadowMCPServerListEntry {
  id: string;
  decision: ShadowMCPDecision;
  evidence: ShadowMCPEvidence;
  matchBreadth: ShadowMCPMatchBreadth;
  roleIds: string[];
  createdAt: string;
  createdBy: string;
  sourceRequestId?: string;
  reason?: string;
}

export interface ShadowMCPRoleOption {
  id: string;
  name: string;
  description?: string;
  isSystem?: boolean;
}
