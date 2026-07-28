import * as z from "zod/v4-mini";
export type CreateSignedChatAttachmentURLForm2 = {
  /**
   * The ID of the chat attachment
   */
  id: string;
  /**
   * The project ID that the attachment belongs to
   */
  projectId: string;
  /**
   * Time-to-live in seconds (default: 600, max: 3600)
   */
  ttlSeconds?: number | undefined;
};
/** @internal */
export type CreateSignedChatAttachmentURLForm2$Outbound = {
  id: string;
  project_id: string;
  ttl_seconds?: number | undefined;
};
/** @internal */
export declare const CreateSignedChatAttachmentURLForm2$outboundSchema: z.ZodMiniType<
  CreateSignedChatAttachmentURLForm2$Outbound,
  CreateSignedChatAttachmentURLForm2
>;
export declare function createSignedChatAttachmentURLForm2ToJSON(
  createSignedChatAttachmentURLForm2: CreateSignedChatAttachmentURLForm2,
): string;
//# sourceMappingURL=createsignedchatattachmenturlform2.d.ts.map
