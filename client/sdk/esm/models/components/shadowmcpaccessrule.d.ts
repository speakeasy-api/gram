import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const ShadowMCPAccessRuleAccessScope: {
    readonly Organization: "organization";
    readonly Project: "project";
};
export type ShadowMCPAccessRuleAccessScope = ClosedEnum<typeof ShadowMCPAccessRuleAccessScope>;
export declare const Disposition: {
    readonly Allowed: "allowed";
    readonly Denied: "denied";
};
export type Disposition = ClosedEnum<typeof Disposition>;
export declare const ShadowMCPAccessRuleMatchBreadth: {
    readonly FullUrl: "full_url";
    readonly UrlHost: "url_host";
};
export type ShadowMCPAccessRuleMatchBreadth = ClosedEnum<typeof ShadowMCPAccessRuleMatchBreadth>;
export type ShadowMCPAccessRule = {
    accessScope: ShadowMCPAccessRuleAccessScope;
    createdAt: Date;
    createdBy?: string | undefined;
    displayName: string;
    disposition: Disposition;
    id: string;
    matchBreadth: ShadowMCPAccessRuleMatchBreadth;
    matchValue: string;
    observedFullUrl?: string | undefined;
    observedServerIdentity?: string | undefined;
    observedUrlHost?: string | undefined;
    organizationId: string;
    projectId?: string | undefined;
    reason?: string | undefined;
    resourceType: string;
    sourceRequestId?: string | undefined;
    updatedAt: Date;
    updatedBy?: string | undefined;
};
/** @internal */
export declare const ShadowMCPAccessRuleAccessScope$inboundSchema: z.ZodMiniEnum<typeof ShadowMCPAccessRuleAccessScope>;
/** @internal */
export declare const Disposition$inboundSchema: z.ZodMiniEnum<typeof Disposition>;
/** @internal */
export declare const ShadowMCPAccessRuleMatchBreadth$inboundSchema: z.ZodMiniEnum<typeof ShadowMCPAccessRuleMatchBreadth>;
/** @internal */
export declare const ShadowMCPAccessRule$inboundSchema: z.ZodMiniType<ShadowMCPAccessRule, unknown>;
export declare function shadowMCPAccessRuleFromJSON(jsonString: string): SafeParseResult<ShadowMCPAccessRule, SDKValidationError>;
//# sourceMappingURL=shadowmcpaccessrule.d.ts.map