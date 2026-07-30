import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ModelUsage } from "./modelusage.js";
import { ToolUsage } from "./toolusage.js";
/**
 * Aggregated metrics
 */
export type Metrics = {
  /**
   * Average chat request duration in milliseconds
   */
  avgChatDurationMs: number;
  /**
   * Average tokens per chat request
   */
  avgTokensPerRequest: number;
  /**
   * Average tool call duration in milliseconds
   */
  avgToolDurationMs: number;
  /**
   * Number of distinct models used (project scope only)
   */
  distinctModels: number;
  /**
   * Number of distinct providers used (project scope only)
   */
  distinctProviders: number;
  /**
   * Requests that completed naturally
   */
  finishReasonStop: number;
  /**
   * Requests that resulted in tool calls
   */
  finishReasonToolCalls: number;
  /**
   * Earliest activity timestamp in Unix nanoseconds
   */
  firstSeenUnixNano: string;
  /**
   * Latest activity timestamp in Unix nanoseconds
   */
  lastSeenUnixNano: string;
  /**
   * List of models used with call counts
   */
  models: Array<ModelUsage>;
  /**
   * Failed tool calls (4xx/5xx status)
   */
  toolCallFailure: number;
  /**
   * Successful tool calls (2xx status)
   */
  toolCallSuccess: number;
  /**
   * List of tools used with success/failure counts
   */
  tools: Array<ToolUsage>;
  /**
   * Total number of chat requests
   */
  totalChatRequests: number;
  /**
   * Number of unique chat sessions (project scope only)
   */
  totalChats: number;
  /**
   * Sum of input tokens used
   */
  totalInputTokens: number;
  /**
   * Sum of output tokens used
   */
  totalOutputTokens: number;
  /**
   * Sum of all tokens used
   */
  totalTokens: number;
  /**
   * Total number of tool calls
   */
  totalToolCalls: number;
};
/** @internal */
export declare const Metrics$inboundSchema: z.ZodType<
  Metrics,
  z.ZodTypeDef,
  unknown
>;
export declare function metricsFromJSON(
  jsonString: string,
): SafeParseResult<Metrics, SDKValidationError>;
//# sourceMappingURL=metrics.d.ts.map
