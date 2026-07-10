import * as z from "zod/v4-mini";
import {
  PublishPluginsRequestBody,
  PublishPluginsRequestBody$Outbound,
} from "../components/publishpluginsrequestbody.js";
export type PublishPluginsSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type PublishPluginsRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  publishPluginsRequestBody: PublishPluginsRequestBody;
};
/** @internal */
export type PublishPluginsSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const PublishPluginsSecurity$outboundSchema: z.ZodMiniType<
  PublishPluginsSecurity$Outbound,
  PublishPluginsSecurity
>;
export declare function publishPluginsSecurityToJSON(
  publishPluginsSecurity: PublishPluginsSecurity,
): string;
/** @internal */
export type PublishPluginsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  PublishPluginsRequestBody: PublishPluginsRequestBody$Outbound;
};
/** @internal */
export declare const PublishPluginsRequest$outboundSchema: z.ZodMiniType<
  PublishPluginsRequest$Outbound,
  PublishPluginsRequest
>;
export declare function publishPluginsRequestToJSON(
  publishPluginsRequest: PublishPluginsRequest,
): string;
//# sourceMappingURL=publishplugins.d.ts.map
