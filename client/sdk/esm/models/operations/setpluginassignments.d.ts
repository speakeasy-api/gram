import * as z from "zod/v4-mini";
import {
  SetPluginAssignmentsForm,
  SetPluginAssignmentsForm$Outbound,
} from "../components/setpluginassignmentsform.js";
export type SetPluginAssignmentsSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type SetPluginAssignmentsRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  setPluginAssignmentsForm: SetPluginAssignmentsForm;
};
/** @internal */
export type SetPluginAssignmentsSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SetPluginAssignmentsSecurity$outboundSchema: z.ZodMiniType<
  SetPluginAssignmentsSecurity$Outbound,
  SetPluginAssignmentsSecurity
>;
export declare function setPluginAssignmentsSecurityToJSON(
  setPluginAssignmentsSecurity: SetPluginAssignmentsSecurity,
): string;
/** @internal */
export type SetPluginAssignmentsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  SetPluginAssignmentsForm: SetPluginAssignmentsForm$Outbound;
};
/** @internal */
export declare const SetPluginAssignmentsRequest$outboundSchema: z.ZodMiniType<
  SetPluginAssignmentsRequest$Outbound,
  SetPluginAssignmentsRequest
>;
export declare function setPluginAssignmentsRequestToJSON(
  setPluginAssignmentsRequest: SetPluginAssignmentsRequest,
): string;
//# sourceMappingURL=setpluginassignments.d.ts.map
