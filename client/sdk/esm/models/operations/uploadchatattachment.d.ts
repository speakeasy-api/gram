import * as z from "zod/v4-mini";
export type UploadChatAttachmentSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UploadChatAttachmentSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UploadChatAttachmentSecurityOption3 = {
  chatSessionsTokenHeaderGramChatSession: string;
  projectSlugHeaderGramProject: string;
};
export type UploadChatAttachmentSecurity = {
  option1?: UploadChatAttachmentSecurityOption1 | undefined;
  option2?: UploadChatAttachmentSecurityOption2 | undefined;
  option3?: UploadChatAttachmentSecurityOption3 | undefined;
};
export type UploadChatAttachmentRequest = {
  contentLength: number;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * Chat Sessions token header
   */
  gramChatSession?: string | undefined;
};
/** @internal */
export type UploadChatAttachmentSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UploadChatAttachmentSecurityOption1$outboundSchema: z.ZodMiniType<
  UploadChatAttachmentSecurityOption1$Outbound,
  UploadChatAttachmentSecurityOption1
>;
export declare function uploadChatAttachmentSecurityOption1ToJSON(
  uploadChatAttachmentSecurityOption1: UploadChatAttachmentSecurityOption1,
): string;
/** @internal */
export type UploadChatAttachmentSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UploadChatAttachmentSecurityOption2$outboundSchema: z.ZodMiniType<
  UploadChatAttachmentSecurityOption2$Outbound,
  UploadChatAttachmentSecurityOption2
>;
export declare function uploadChatAttachmentSecurityOption2ToJSON(
  uploadChatAttachmentSecurityOption2: UploadChatAttachmentSecurityOption2,
): string;
/** @internal */
export type UploadChatAttachmentSecurityOption3$Outbound = {
  "chat_sessions_token_header_Gram-Chat-Session": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UploadChatAttachmentSecurityOption3$outboundSchema: z.ZodMiniType<
  UploadChatAttachmentSecurityOption3$Outbound,
  UploadChatAttachmentSecurityOption3
>;
export declare function uploadChatAttachmentSecurityOption3ToJSON(
  uploadChatAttachmentSecurityOption3: UploadChatAttachmentSecurityOption3,
): string;
/** @internal */
export type UploadChatAttachmentSecurity$Outbound = {
  Option1?: UploadChatAttachmentSecurityOption1$Outbound | undefined;
  Option2?: UploadChatAttachmentSecurityOption2$Outbound | undefined;
  Option3?: UploadChatAttachmentSecurityOption3$Outbound | undefined;
};
/** @internal */
export declare const UploadChatAttachmentSecurity$outboundSchema: z.ZodMiniType<
  UploadChatAttachmentSecurity$Outbound,
  UploadChatAttachmentSecurity
>;
export declare function uploadChatAttachmentSecurityToJSON(
  uploadChatAttachmentSecurity: UploadChatAttachmentSecurity,
): string;
/** @internal */
export type UploadChatAttachmentRequest$Outbound = {
  "Content-Length": number;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Chat-Session"?: string | undefined;
};
/** @internal */
export declare const UploadChatAttachmentRequest$outboundSchema: z.ZodMiniType<
  UploadChatAttachmentRequest$Outbound,
  UploadChatAttachmentRequest
>;
export declare function uploadChatAttachmentRequestToJSON(
  uploadChatAttachmentRequest: UploadChatAttachmentRequest,
): string;
//# sourceMappingURL=uploadchatattachment.d.ts.map
