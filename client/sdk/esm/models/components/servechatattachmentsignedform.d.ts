import * as z from "zod/v3";
export type ServeChatAttachmentSignedForm = {
  /**
   * The signed JWT token
   */
  token: string;
};
/** @internal */
export type ServeChatAttachmentSignedForm$Outbound = {
  token: string;
};
/** @internal */
export declare const ServeChatAttachmentSignedForm$outboundSchema: z.ZodType<
  ServeChatAttachmentSignedForm$Outbound,
  z.ZodTypeDef,
  ServeChatAttachmentSignedForm
>;
export declare function serveChatAttachmentSignedFormToJSON(
  serveChatAttachmentSignedForm: ServeChatAttachmentSignedForm,
): string;
//# sourceMappingURL=servechatattachmentsignedform.d.ts.map
