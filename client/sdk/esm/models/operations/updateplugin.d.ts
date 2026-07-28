import * as z from "zod/v4-mini";
import {
  UpdatePluginForm,
  UpdatePluginForm$Outbound,
} from "../components/updatepluginform.js";
export type UpdatePluginSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type UpdatePluginRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  updatePluginForm: UpdatePluginForm;
};
/** @internal */
export type UpdatePluginSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdatePluginSecurity$outboundSchema: z.ZodMiniType<
  UpdatePluginSecurity$Outbound,
  UpdatePluginSecurity
>;
export declare function updatePluginSecurityToJSON(
  updatePluginSecurity: UpdatePluginSecurity,
): string;
/** @internal */
export type UpdatePluginRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdatePluginForm: UpdatePluginForm$Outbound;
};
/** @internal */
export declare const UpdatePluginRequest$outboundSchema: z.ZodMiniType<
  UpdatePluginRequest$Outbound,
  UpdatePluginRequest
>;
export declare function updatePluginRequestToJSON(
  updatePluginRequest: UpdatePluginRequest,
): string;
//# sourceMappingURL=updateplugin.d.ts.map
