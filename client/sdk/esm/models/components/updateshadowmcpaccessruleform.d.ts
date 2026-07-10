import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export declare const UpdateShadowMCPAccessRuleFormAccessScope: {
  readonly Organization: "organization";
  readonly Project: "project";
};
export type UpdateShadowMCPAccessRuleFormAccessScope = ClosedEnum<
  typeof UpdateShadowMCPAccessRuleFormAccessScope
>;
export declare const UpdateShadowMCPAccessRuleFormDisposition: {
  readonly Allowed: "allowed";
  readonly Denied: "denied";
};
export type UpdateShadowMCPAccessRuleFormDisposition = ClosedEnum<
  typeof UpdateShadowMCPAccessRuleFormDisposition
>;
export declare const UpdateShadowMCPAccessRuleFormMatchBreadth: {
  readonly FullUrl: "full_url";
  readonly UrlHost: "url_host";
};
export type UpdateShadowMCPAccessRuleFormMatchBreadth = ClosedEnum<
  typeof UpdateShadowMCPAccessRuleFormMatchBreadth
>;
export type UpdateShadowMCPAccessRuleForm = {
  accessScope: UpdateShadowMCPAccessRuleFormAccessScope;
  displayName: string;
  disposition: UpdateShadowMCPAccessRuleFormDisposition;
  id: string;
  matchBreadth: UpdateShadowMCPAccessRuleFormMatchBreadth;
  matchValue: string;
  observedFullUrl?: string | undefined;
  observedServerIdentity?: string | undefined;
  observedUrlHost?: string | undefined;
  projectId?: string | undefined;
  reason?: string | undefined;
};
/** @internal */
export declare const UpdateShadowMCPAccessRuleFormAccessScope$outboundSchema: z.ZodMiniEnum<
  typeof UpdateShadowMCPAccessRuleFormAccessScope
>;
/** @internal */
export declare const UpdateShadowMCPAccessRuleFormDisposition$outboundSchema: z.ZodMiniEnum<
  typeof UpdateShadowMCPAccessRuleFormDisposition
>;
/** @internal */
export declare const UpdateShadowMCPAccessRuleFormMatchBreadth$outboundSchema: z.ZodMiniEnum<
  typeof UpdateShadowMCPAccessRuleFormMatchBreadth
>;
/** @internal */
export type UpdateShadowMCPAccessRuleForm$Outbound = {
  access_scope: string;
  display_name: string;
  disposition: string;
  id: string;
  match_breadth: string;
  match_value: string;
  observed_full_url?: string | undefined;
  observed_server_identity?: string | undefined;
  observed_url_host?: string | undefined;
  project_id?: string | undefined;
  reason?: string | undefined;
};
/** @internal */
export declare const UpdateShadowMCPAccessRuleForm$outboundSchema: z.ZodMiniType<
  UpdateShadowMCPAccessRuleForm$Outbound,
  UpdateShadowMCPAccessRuleForm
>;
export declare function updateShadowMCPAccessRuleFormToJSON(
  updateShadowMCPAccessRuleForm: UpdateShadowMCPAccessRuleForm,
): string;
//# sourceMappingURL=updateshadowmcpaccessruleform.d.ts.map
