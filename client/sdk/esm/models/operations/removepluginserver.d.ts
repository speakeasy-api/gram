import * as z from "zod/v4-mini";
export type RemovePluginServerSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type RemovePluginServerRequest = {
  /**
   * The plugin server ID to remove.
   */
  id: string;
  pluginId: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type RemovePluginServerSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RemovePluginServerSecurity$outboundSchema: z.ZodMiniType<
  RemovePluginServerSecurity$Outbound,
  RemovePluginServerSecurity
>;
export declare function removePluginServerSecurityToJSON(
  removePluginServerSecurity: RemovePluginServerSecurity,
): string;
/** @internal */
export type RemovePluginServerRequest$Outbound = {
  id: string;
  plugin_id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const RemovePluginServerRequest$outboundSchema: z.ZodMiniType<
  RemovePluginServerRequest$Outbound,
  RemovePluginServerRequest
>;
export declare function removePluginServerRequestToJSON(
  removePluginServerRequest: RemovePluginServerRequest,
): string;
//# sourceMappingURL=removepluginserver.d.ts.map
