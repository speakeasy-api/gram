import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Current request status.
 */
export declare const RiskPolicyBypassRequestStatus: {
    readonly Requested: "requested";
    readonly Approved: "approved";
    readonly Denied: "denied";
    readonly Revoked: "revoked";
};
/**
 * Current request status.
 */
export type RiskPolicyBypassRequestStatus = ClosedEnum<typeof RiskPolicyBypassRequestStatus>;
export type RiskPolicyBypassRequest = {
    /**
     * Creation timestamp.
     */
    createdAt: Date;
    /**
     * Decision timestamp.
     */
    decidedAt?: Date | undefined;
    /**
     * User ID that approved, denied, or revoked the request.
     */
    decidedBy?: string | undefined;
    /**
     * Principal URNs granted when approved.
     */
    grantedPrincipalUrns: Array<string>;
    /**
     * The bypass request ID.
     */
    id: string;
    /**
     * Requester note.
     */
    note?: string | undefined;
    /**
     * The risk policy ID.
     */
    policyId: string;
    /**
     * Requester email when known.
     */
    requesterEmail?: string | undefined;
    /**
     * Requester user ID.
     */
    requesterUserId: string;
    /**
     * Current request status.
     */
    status: RiskPolicyBypassRequestStatus;
    /**
     * Selector dimensions for the request target.
     */
    targetDimensions: {
        [k: string]: string;
    };
    /**
     * Canonical key for the target.
     */
    targetKey?: string | undefined;
    /**
     * Optional target namespace for the request, such as server_url.
     */
    targetKind?: string | undefined;
    /**
     * Optional display label for the target.
     */
    targetLabel?: string | undefined;
    /**
     * Last update timestamp.
     */
    updatedAt: Date;
};
/** @internal */
export declare const RiskPolicyBypassRequestStatus$inboundSchema: z.ZodMiniEnum<typeof RiskPolicyBypassRequestStatus>;
/** @internal */
export declare const RiskPolicyBypassRequest$inboundSchema: z.ZodMiniType<RiskPolicyBypassRequest, unknown>;
export declare function riskPolicyBypassRequestFromJSON(jsonString: string): SafeParseResult<RiskPolicyBypassRequest, SDKValidationError>;
//# sourceMappingURL=riskpolicybypassrequest.d.ts.map