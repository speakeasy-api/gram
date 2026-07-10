import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export declare const CreateShadowMCPAccessRuleFormAccessScope: {
    readonly Organization: "organization";
    readonly Project: "project";
};
export type CreateShadowMCPAccessRuleFormAccessScope = ClosedEnum<typeof CreateShadowMCPAccessRuleFormAccessScope>;
export declare const CreateShadowMCPAccessRuleFormDisposition: {
    readonly Allowed: "allowed";
    readonly Denied: "denied";
};
export type CreateShadowMCPAccessRuleFormDisposition = ClosedEnum<typeof CreateShadowMCPAccessRuleFormDisposition>;
export declare const CreateShadowMCPAccessRuleFormMatchBreadth: {
    readonly FullUrl: "full_url";
    readonly UrlHost: "url_host";
};
export type CreateShadowMCPAccessRuleFormMatchBreadth = ClosedEnum<typeof CreateShadowMCPAccessRuleFormMatchBreadth>;
export type CreateShadowMCPAccessRuleForm = {
    accessScope: CreateShadowMCPAccessRuleFormAccessScope;
    displayName: string;
    disposition: CreateShadowMCPAccessRuleFormDisposition;
    matchBreadth: CreateShadowMCPAccessRuleFormMatchBreadth;
    matchValue: string;
    observedFullUrl?: string | undefined;
    observedServerIdentity?: string | undefined;
    observedUrlHost?: string | undefined;
    projectId?: string | undefined;
    /**
     * Project ids to create project-scoped rules for. Empty uses project_id for single-rule creation.
     */
    projectIds?: Array<string> | undefined;
    reason?: string | undefined;
};
/** @internal */
export declare const CreateShadowMCPAccessRuleFormAccessScope$outboundSchema: z.ZodMiniEnum<typeof CreateShadowMCPAccessRuleFormAccessScope>;
/** @internal */
export declare const CreateShadowMCPAccessRuleFormDisposition$outboundSchema: z.ZodMiniEnum<typeof CreateShadowMCPAccessRuleFormDisposition>;
/** @internal */
export declare const CreateShadowMCPAccessRuleFormMatchBreadth$outboundSchema: z.ZodMiniEnum<typeof CreateShadowMCPAccessRuleFormMatchBreadth>;
/** @internal */
export type CreateShadowMCPAccessRuleForm$Outbound = {
    access_scope: string;
    display_name: string;
    disposition: string;
    match_breadth: string;
    match_value: string;
    observed_full_url?: string | undefined;
    observed_server_identity?: string | undefined;
    observed_url_host?: string | undefined;
    project_id?: string | undefined;
    project_ids?: Array<string> | undefined;
    reason?: string | undefined;
};
/** @internal */
export declare const CreateShadowMCPAccessRuleForm$outboundSchema: z.ZodMiniType<CreateShadowMCPAccessRuleForm$Outbound, CreateShadowMCPAccessRuleForm>;
export declare function createShadowMCPAccessRuleFormToJSON(createShadowMCPAccessRuleForm: CreateShadowMCPAccessRuleForm): string;
//# sourceMappingURL=createshadowmcpaccessruleform.d.ts.map