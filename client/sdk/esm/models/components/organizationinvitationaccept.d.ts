import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Invitation lifecycle state.
 */
export declare const State: {
    readonly Pending: "pending";
    readonly Accepted: "accepted";
    readonly Expired: "expired";
    readonly Revoked: "revoked";
};
/**
 * Invitation lifecycle state.
 */
export type State = ClosedEnum<typeof State>;
export type OrganizationInvitationAccept = {
    /**
     * URL to complete acceptance in WorkOS (may be empty when not actionable).
     */
    acceptInvitationUrl: string;
    /**
     * Invitee email address.
     */
    email: string;
    /**
     * Gram organization display name when the org is linked in Gram; empty if unknown.
     */
    organizationName: string;
    /**
     * Invitation lifecycle state.
     */
    state: State;
};
/** @internal */
export declare const State$inboundSchema: z.ZodMiniEnum<typeof State>;
/** @internal */
export declare const OrganizationInvitationAccept$inboundSchema: z.ZodMiniType<OrganizationInvitationAccept, unknown>;
export declare function organizationInvitationAcceptFromJSON(jsonString: string): SafeParseResult<OrganizationInvitationAccept, SDKValidationError>;
//# sourceMappingURL=organizationinvitationaccept.d.ts.map