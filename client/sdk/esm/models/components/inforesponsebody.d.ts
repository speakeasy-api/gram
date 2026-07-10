import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OrganizationEntry } from "./organizationentry.js";
export type InfoResponseBody = {
    activeOrganizationId: string;
    gramAccountType: string;
    /**
     * Whether the organization has an active billing subscription
     */
    hasActiveSubscription: boolean;
    isAdmin: boolean;
    organizations: Array<OrganizationEntry>;
    userDisplayName?: string | undefined;
    userEmail: string;
    userId: string;
    userPhotoUrl?: string | undefined;
    userSignature?: string | undefined;
    /**
     * Whether the organization is whitelisted to access the platform
     */
    whitelisted: boolean;
};
/** @internal */
export declare const InfoResponseBody$inboundSchema: z.ZodMiniType<InfoResponseBody, unknown>;
export declare function infoResponseBodyFromJSON(jsonString: string): SafeParseResult<InfoResponseBody, SDKValidationError>;
//# sourceMappingURL=inforesponsebody.d.ts.map