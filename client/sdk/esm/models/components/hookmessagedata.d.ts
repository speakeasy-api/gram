import * as z from "zod/v4-mini";
/**
 * Assistant/user message payload.
 */
export type HookMessageData = {
  /**
   * Message or thinking-block duration in milliseconds, when reported.
   */
  durationMs?: number | undefined;
  /**
   * Message role, e.g. assistant or user.
   */
  role?: string | undefined;
  /**
   * Message text.
   */
  text?: string | undefined;
};
/** @internal */
export type HookMessageData$Outbound = {
  duration_ms?: number | undefined;
  role?: string | undefined;
  text?: string | undefined;
};
/** @internal */
export declare const HookMessageData$outboundSchema: z.ZodMiniType<
  HookMessageData$Outbound,
  HookMessageData
>;
export declare function hookMessageDataToJSON(
  hookMessageData: HookMessageData,
): string;
//# sourceMappingURL=hookmessagedata.d.ts.map
