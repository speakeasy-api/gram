import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const Status: {
    readonly Requested: "requested";
    readonly Approved: "approved";
    readonly Denied: "denied";
};
export type Status = ClosedEnum<typeof Status>;
export type ShadowMCPApprovalRequest = {
    blockReason?: string | undefined;
    blockedCount: number;
    createdAt: Date;
    decidedAt?: Date | undefined;
    decidedBy?: string | undefined;
    decisionNote?: string | undefined;
    firstBlockedAt?: Date | undefined;
    id: string;
    lastBlockedAt?: Date | undefined;
    observedFullUrl?: string | undefined;
    observedName?: string | undefined;
    observedServerIdentity?: string | undefined;
    observedUrlHost?: string | undefined;
    organizationId: string;
    projectId: string;
    requestedAt: Date;
    requesterDisplayName?: string | undefined;
    requesterEmail?: string | undefined;
    requesterUserId?: string | undefined;
    resourceType: string;
    riskPolicyId?: string | undefined;
    riskResultId?: string | undefined;
    status: Status;
    toolCall?: string | undefined;
    toolName?: string | undefined;
    updatedAt: Date;
};
/** @internal */
export declare const Status$inboundSchema: z.ZodMiniEnum<typeof Status>;
/** @internal */
export declare const ShadowMCPApprovalRequest$inboundSchema: z.ZodMiniType<ShadowMCPApprovalRequest, unknown>;
export declare function shadowMCPApprovalRequestFromJSON(jsonString: string): SafeParseResult<ShadowMCPApprovalRequest, SDKValidationError>;
//# sourceMappingURL=shadowmcpapprovalrequest.d.ts.map