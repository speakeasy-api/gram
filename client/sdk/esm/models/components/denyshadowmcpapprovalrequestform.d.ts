import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export declare const DenyShadowMCPApprovalRequestFormMatchBreadth: {
    readonly FullUrl: "full_url";
    readonly UrlHost: "url_host";
};
export type DenyShadowMCPApprovalRequestFormMatchBreadth = ClosedEnum<typeof DenyShadowMCPApprovalRequestFormMatchBreadth>;
export type DenyShadowMCPApprovalRequestForm = {
    createDenyRule: boolean;
    displayName?: string | undefined;
    id: string;
    matchBreadth?: DenyShadowMCPApprovalRequestFormMatchBreadth | undefined;
    matchValue?: string | undefined;
    observedFullUrl?: string | undefined;
    observedServerIdentity?: string | undefined;
    observedUrlHost?: string | undefined;
    /**
     * Project ids to create project-scoped deny rules for. Empty falls back to the request project.
     */
    projectIds?: Array<string> | undefined;
    reason?: string | undefined;
};
/** @internal */
export declare const DenyShadowMCPApprovalRequestFormMatchBreadth$outboundSchema: z.ZodMiniEnum<typeof DenyShadowMCPApprovalRequestFormMatchBreadth>;
/** @internal */
export type DenyShadowMCPApprovalRequestForm$Outbound = {
    create_deny_rule: boolean;
    display_name?: string | undefined;
    id: string;
    match_breadth?: string | undefined;
    match_value?: string | undefined;
    observed_full_url?: string | undefined;
    observed_server_identity?: string | undefined;
    observed_url_host?: string | undefined;
    project_ids?: Array<string> | undefined;
    reason?: string | undefined;
};
/** @internal */
export declare const DenyShadowMCPApprovalRequestForm$outboundSchema: z.ZodMiniType<DenyShadowMCPApprovalRequestForm$Outbound, DenyShadowMCPApprovalRequestForm>;
export declare function denyShadowMCPApprovalRequestFormToJSON(denyShadowMCPApprovalRequestForm: DenyShadowMCPApprovalRequestForm): string;
//# sourceMappingURL=denyshadowmcpapprovalrequestform.d.ts.map