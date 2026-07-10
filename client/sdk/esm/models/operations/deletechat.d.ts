import * as z from "zod/v4-mini";
export type DeleteChatSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteChatRequest = {
  /**
   * The ID of the chat to delete
   */
  id: string;
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
export type DeleteChatSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteChatSecurity$outboundSchema: z.ZodMiniType<
  DeleteChatSecurity$Outbound,
  DeleteChatSecurity
>;
export declare function deleteChatSecurityToJSON(
  deleteChatSecurity: DeleteChatSecurity,
): string;
/** @internal */
export type DeleteChatRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteChatRequest$outboundSchema: z.ZodMiniType<
  DeleteChatRequest$Outbound,
  DeleteChatRequest
>;
export declare function deleteChatRequestToJSON(
  deleteChatRequest: DeleteChatRequest,
): string;
//# sourceMappingURL=deletechat.d.ts.map
