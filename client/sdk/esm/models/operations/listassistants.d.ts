import * as z from "zod/v4-mini";
export type ListAssistantsSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ListAssistantsRequest = {
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
export type ListAssistantsSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAssistantsSecurity$outboundSchema: z.ZodMiniType<
  ListAssistantsSecurity$Outbound,
  ListAssistantsSecurity
>;
export declare function listAssistantsSecurityToJSON(
  listAssistantsSecurity: ListAssistantsSecurity,
): string;
/** @internal */
export type ListAssistantsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListAssistantsRequest$outboundSchema: z.ZodMiniType<
  ListAssistantsRequest$Outbound,
  ListAssistantsRequest
>;
export declare function listAssistantsRequestToJSON(
  listAssistantsRequest: ListAssistantsRequest,
): string;
//# sourceMappingURL=listassistants.d.ts.map
