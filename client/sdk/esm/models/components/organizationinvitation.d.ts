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
export type OrganizationInvitation = {
    /**
     * When the invitation was accepted.
     */
    acceptedAt?: Date | undefined;
    createdAt: Date;
    /**
     * Invitee email address.
     */
    email: string;
    /**
     * When the invitation expires.
     */
    expiresAt?: Date | undefined;
    /**
     * WorkOS invitation ID.
     */
    id: string;
    /**
     * Gram user ID of the inviter, when known.
     */
    inviterUserId?: string | undefined;
    /**
     * When the invitation was revoked.
     */
    revokedAt?: Date | undefined;
    /**
     * WorkOS role slug assigned when the invite is accepted.
     */
    roleSlug?: string | undefined;
    /**
     * Invitation lifecycle state.
     */
    state: State;
    updatedAt: Date;
};
/** @internal */
export declare const State$inboundSchema: z.ZodMiniEnum<typeof State>;
/** @internal */
export declare const OrganizationInvitation$inboundSchema: z.ZodMiniType<OrganizationInvitation, unknown>;
export declare function organizationInvitationFromJSON(jsonString: string): SafeParseResult<OrganizationInvitation, SDKValidationError>;
//# sourceMappingURL=organizationinvitation.d.ts.map