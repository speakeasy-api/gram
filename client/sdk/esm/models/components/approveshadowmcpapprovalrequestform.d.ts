import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export declare const AccessScope: {
    readonly Organization: "organization";
    readonly Project: "project";
};
export type AccessScope = ClosedEnum<typeof AccessScope>;
export declare const MatchBreadth: {
    readonly FullUrl: "full_url";
    readonly UrlHost: "url_host";
};
export type MatchBreadth = ClosedEnum<typeof MatchBreadth>;
export type ApproveShadowMCPApprovalRequestForm = {
    accessScope: AccessScope;
    displayName: string;
    id: string;
    matchBreadth: MatchBreadth;
    matchValue: string;
    observedFullUrl?: string | undefined;
    observedServerIdentity?: string | undefined;
    observedUrlHost?: string | undefined;
    /**
     * Project ids to create project-scoped rules for. Empty falls back to the request project.
     */
    projectIds?: Array<string> | undefined;
    reason?: string | undefined;
};
/** @internal */
export declare const AccessScope$outboundSchema: z.ZodMiniEnum<typeof AccessScope>;
/** @internal */
export declare const MatchBreadth$outboundSchema: z.ZodMiniEnum<typeof MatchBreadth>;
/** @internal */
export type ApproveShadowMCPApprovalRequestForm$Outbound = {
    access_scope: string;
    display_name: string;
    id: string;
    match_breadth: string;
    match_value: string;
    observed_full_url?: string | undefined;
    observed_server_identity?: string | undefined;
    observed_url_host?: string | undefined;
    project_ids?: Array<string> | undefined;
    reason?: string | undefined;
};
/** @internal */
export declare const ApproveShadowMCPApprovalRequestForm$outboundSchema: z.ZodMiniType<ApproveShadowMCPApprovalRequestForm$Outbound, ApproveShadowMCPApprovalRequestForm>;
export declare function approveShadowMCPApprovalRequestFormToJSON(approveShadowMCPApprovalRequestForm: ApproveShadowMCPApprovalRequestForm): string;
//# sourceMappingURL=approveshadowmcpapprovalrequestform.d.ts.map