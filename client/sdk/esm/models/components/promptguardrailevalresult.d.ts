import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { PromptGuardrailMessageVerdict } from "./promptguardrailmessageverdict.js";
/**
 * The result of replaying a prompt guardrail against one chat session. Read-only: no findings are persisted.
 */
export type PromptGuardrailEvalResult = {
  /**
   * The chat session that was replayed.
   */
  chatId: string;
  /**
   * True when the guardrail flagged at least one in-scope message.
   */
  flagged: boolean;
  /**
   * Number of in-scope messages the judge evaluated.
   */
  judgedCount: number;
  /**
   * Total OpenRouter cost across in-scope judge calls, in USD.
   */
  totalCostUsd: number;
  /**
   * Aggregate judge latency overhead across in-scope messages, computed as the sum of per-message judge latencies.
   */
  totalLatencyMs: number;
  /**
   * Per-message verdicts for in-scope messages, ordered by seq.
   */
  verdicts: Array<PromptGuardrailMessageVerdict>;
};
/** @internal */
export declare const PromptGuardrailEvalResult$inboundSchema: z.ZodMiniType<
  PromptGuardrailEvalResult,
  unknown
>;
export declare function promptGuardrailEvalResultFromJSON(
  jsonString: string,
): SafeParseResult<PromptGuardrailEvalResult, SDKValidationError>;
//# sourceMappingURL=promptguardrailevalresult.d.ts.map
