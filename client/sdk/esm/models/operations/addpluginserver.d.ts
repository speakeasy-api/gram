import * as z from "zod/v4-mini";
import {
  AddPluginServerForm,
  AddPluginServerForm$Outbound,
} from "../components/addpluginserverform.js";
export type AddPluginServerSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type AddPluginServerRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  addPluginServerForm: AddPluginServerForm;
};
/** @internal */
export type AddPluginServerSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const AddPluginServerSecurity$outboundSchema: z.ZodMiniType<
  AddPluginServerSecurity$Outbound,
  AddPluginServerSecurity
>;
export declare function addPluginServerSecurityToJSON(
  addPluginServerSecurity: AddPluginServerSecurity,
): string;
/** @internal */
export type AddPluginServerRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  AddPluginServerForm: AddPluginServerForm$Outbound;
};
/** @internal */
export declare const AddPluginServerRequest$outboundSchema: z.ZodMiniType<
  AddPluginServerRequest$Outbound,
  AddPluginServerRequest
>;
export declare function addPluginServerRequestToJSON(
  addPluginServerRequest: AddPluginServerRequest,
): string;
//# sourceMappingURL=addpluginserver.d.ts.map
