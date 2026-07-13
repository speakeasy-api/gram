import * as z from "zod/v4-mini";
import {
  ClaudeHookPayload,
  ClaudeHookPayload$Outbound,
} from "../components/claudehookpayload.js";
export type HooksNumberClaudeRequest = {
  /**
   * Optional API key for plugin-driven attribution.
   */
  gramKey?: string | undefined;
  /**
   * Optional project slug for plugin-driven attribution.
   */
  gramProject?: string | undefined;
  /**
   * Optional endpoint hostname supplied by the Gram hook plugin.
   */
  xGramHookHostname?: string | undefined;
  /**
   * Optional per-invocation token reused across retries so the server stores a redelivered event exactly once.
   */
  idempotencyKey?: string | undefined;
  claudeHookPayload: ClaudeHookPayload;
};
/** @internal */
export type HooksNumberClaudeRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  "X-Gram-Hook-Hostname"?: string | undefined;
  "Idempotency-Key"?: string | undefined;
  ClaudeHookPayload: ClaudeHookPayload$Outbound;
};
/** @internal */
export declare const HooksNumberClaudeRequest$outboundSchema: z.ZodMiniType<
  HooksNumberClaudeRequest$Outbound,
  HooksNumberClaudeRequest
>;
export declare function hooksNumberClaudeRequestToJSON(
  hooksNumberClaudeRequest: HooksNumberClaudeRequest,
): string;
//# sourceMappingURL=hooksnumberclaude.d.ts.map
