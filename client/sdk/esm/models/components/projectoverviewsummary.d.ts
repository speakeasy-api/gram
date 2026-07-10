import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { LLMClientUsage } from "./llmclientusage.js";
import { TopServer } from "./topserver.js";
import { TopUser } from "./topuser.js";
/**
 * Aggregated project-level summary metrics for a time period
 */
export type ProjectOverviewSummary = {
  /**
   * Number of MCP servers with at least one tool call in the time period
   */
  activeServersCount: number;
  /**
   * Number of unique users with activity in the time period
   */
  activeUsersCount: number;
  /**
   * Number of failed chat sessions
   */
  failedChats: number;
  /**
   * Number of failed tool calls
   */
  failedToolCalls: number;
  /**
   * Breakdown of messages/activity by LLM client/agent
   */
  llmClientBreakdown: Array<LLMClientUsage>;
  /**
   * Number of resolved chat sessions
   */
  resolvedChats: number;
  /**
   * Top 10 MCP servers by tool call count
   */
  topServers: Array<TopServer>;
  /**
   * Top 10 users by activity (# of messages or tool calls depending on metrics_mode)
   */
  topUsers: Array<TopUser>;
  /**
   * Total number of chat sessions
   */
  totalChats: number;
  /**
   * Total number of tool calls
   */
  totalToolCalls: number;
};
/** @internal */
export declare const ProjectOverviewSummary$inboundSchema: z.ZodMiniType<
  ProjectOverviewSummary,
  unknown
>;
export declare function projectOverviewSummaryFromJSON(
  jsonString: string,
): SafeParseResult<ProjectOverviewSummary, SDKValidationError>;
//# sourceMappingURL=projectoverviewsummary.d.ts.map
