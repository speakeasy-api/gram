import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListShadowMCPAccessRulesSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export declare const Disposition: {
    readonly Allowed: "allowed";
    readonly Denied: "denied";
};
export type Disposition = ClosedEnum<typeof Disposition>;
export declare const AccessScope: {
    readonly Organization: "organization";
    readonly Project: "project";
};
export type AccessScope = ClosedEnum<typeof AccessScope>;
export type ListShadowMCPAccessRulesRequest = {
    disposition?: Disposition | undefined;
    accessScope?: AccessScope | undefined;
    projectId?: string | undefined;
    limit?: number | undefined;
    /**
     * Cursor for the next page of results.
     */
    cursor?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type ListShadowMCPAccessRulesSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListShadowMCPAccessRulesSecurity$outboundSchema: z.ZodMiniType<ListShadowMCPAccessRulesSecurity$Outbound, ListShadowMCPAccessRulesSecurity>;
export declare function listShadowMCPAccessRulesSecurityToJSON(listShadowMCPAccessRulesSecurity: ListShadowMCPAccessRulesSecurity): string;
/** @internal */
export declare const Disposition$outboundSchema: z.ZodMiniEnum<typeof Disposition>;
/** @internal */
export declare const AccessScope$outboundSchema: z.ZodMiniEnum<typeof AccessScope>;
/** @internal */
export type ListShadowMCPAccessRulesRequest$Outbound = {
    disposition?: string | undefined;
    access_scope?: string | undefined;
    project_id?: string | undefined;
    limit: number;
    cursor?: string | undefined;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListShadowMCPAccessRulesRequest$outboundSchema: z.ZodMiniType<ListShadowMCPAccessRulesRequest$Outbound, ListShadowMCPAccessRulesRequest>;
export declare function listShadowMCPAccessRulesRequestToJSON(listShadowMCPAccessRulesRequest: ListShadowMCPAccessRulesRequest): string;
//# sourceMappingURL=listshadowmcpaccessrules.d.ts.map