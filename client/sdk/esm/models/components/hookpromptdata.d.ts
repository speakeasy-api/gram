import * as z from "zod/v4-mini";
/**
 * Prompt feature payload.
 */
export type HookPromptData = {
  /**
   * User prompt text.
   */
  text?: string | undefined;
};
/** @internal */
export type HookPromptData$Outbound = {
  text?: string | undefined;
};
/** @internal */
export declare const HookPromptData$outboundSchema: z.ZodMiniType<
  HookPromptData$Outbound,
  HookPromptData
>;
export declare function hookPromptDataToJSON(
  hookPromptData: HookPromptData,
): string;
//# sourceMappingURL=hookpromptdata.d.ts.map
