import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { HookSourceUsage } from "./hooksourceusage.js";
import { ToolUsage } from "./toolusage.js";
import { UserAccount } from "./useraccount.js";
/**
 * Aggregated usage summary for a single user
 */
export type UserSummary = {
  /**
   * Distinct account types observed for this user ('team', 'personal')
   */
  accountTypes?: Array<string> | undefined;
  /**
   * Linked AI accounts for this user (team and personal, across providers)
   */
  accounts?: Array<UserAccount> | undefined;
  /**
   * Average tokens per chat request
   */
  avgTokensPerRequest: number;
  /**
   * Sum of cache creation input tokens
   */
  cacheCreationInputTokens: number;
  /**
   * Sum of cache read input tokens
   */
  cacheReadInputTokens: number;
  /**
   * Earliest activity timestamp in Unix nanoseconds
   */
  firstSeenUnixNano: string;
  /**
   * Per-hook-source usage breakdown
   */
  hookSources: Array<HookSourceUsage>;
  /**
   * Latest activity timestamp in Unix nanoseconds
   */
  lastSeenUnixNano: string;
  /**
   * Failed tool calls (4xx/5xx status)
   */
  toolCallFailure: number;
  /**
   * Successful tool calls (2xx status)
   */
  toolCallSuccess: number;
  /**
   * Per-tool usage breakdown
   */
  tools: Array<ToolUsage>;
  /**
   * Total number of chat completion requests
   */
  totalChatRequests: number;
  /**
   * Number of unique chat sessions
   */
  totalChats: number;
  /**
   * Total cost of all requests
   */
  totalCost: number;
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
  /**
   * User email associated with this usage, when present
   */
  userEmail: string;
  /**
   * User identifier (user_id or external_user_id depending on group_by)
   */
  userId: string;
};
/** @internal */
export declare const UserSummary$inboundSchema: z.ZodMiniType<
  UserSummary,
  unknown
>;
export declare function userSummaryFromJSON(
  jsonString: string,
): SafeParseResult<UserSummary, SDKValidationError>;
//# sourceMappingURL=usersummary.d.ts.map
