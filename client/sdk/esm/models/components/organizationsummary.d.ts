import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Gram account tier.
 */
export declare const GramAccountType: {
    readonly Free: "free";
    readonly Pro: "pro";
    readonly Enterprise: "enterprise";
};
/**
 * Gram account tier.
 */
export type GramAccountType = ClosedEnum<typeof GramAccountType>;
export type OrganizationSummary = {
    createdAt: Date;
    /**
     * When the organization was disabled, if applicable.
     */
    disabledAt?: Date | undefined;
    /**
     * Gram account tier.
     */
    gramAccountType: GramAccountType;
    /**
     * Gram organization ID.
     */
    id: string;
    /**
     * Organization display name.
     */
    name: string;
    /**
     * Organization slug.
     */
    slug: string;
    updatedAt: Date;
    /**
     * Whether the organization is whitelisted.
     */
    whitelisted: boolean;
    /**
     * WorkOS organization ID, when linked.
     */
    workosId?: string | undefined;
};
/** @internal */
export declare const GramAccountType$inboundSchema: z.ZodMiniEnum<typeof GramAccountType>;
/** @internal */
export declare const OrganizationSummary$inboundSchema: z.ZodMiniType<OrganizationSummary, unknown>;
export declare function organizationSummaryFromJSON(jsonString: string): SafeParseResult<OrganizationSummary, SDKValidationError>;
//# sourceMappingURL=organizationsummary.d.ts.map