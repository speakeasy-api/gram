import * as z from "zod/v4-mini";
export type KickoffMessageRequestBody = {
  /**
   * The assistant to greet from.
   */
  assistantId: string;
  /**
   * Conversation key to greet within — pass the same value used for sendMessage so the assistant greets inside the existing thread (and can recap it).
   */
  correlationId: string;
};
/** @internal */
export type KickoffMessageRequestBody$Outbound = {
  assistant_id: string;
  correlation_id: string;
};
/** @internal */
export declare const KickoffMessageRequestBody$outboundSchema: z.ZodMiniType<
  KickoffMessageRequestBody$Outbound,
  KickoffMessageRequestBody
>;
export declare function kickoffMessageRequestBodyToJSON(
  kickoffMessageRequestBody: KickoffMessageRequestBody,
): string;
//# sourceMappingURL=kickoffmessagerequestbody.d.ts.map
