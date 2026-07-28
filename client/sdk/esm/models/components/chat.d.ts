import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AgentUsage } from "./agentusage.js";
import { ChatMessage } from "./chatmessage.js";
import { ChatTotals } from "./chattotals.js";
import { RiskSegment } from "./risksegment.js";
export type Chat = {
  /**
   * Email of the AI account that produced the chat, resolved from the linked AI account. May differ from the employee's work email (e.g. a personal account).
   */
  accountEmail?: string | undefined;
  /**
   * Account type that produced the chat ('team', 'personal', or empty), resolved from the linked AI account.
   */
  accountType?: string | undefined;
  agentUsage?: AgentUsage | undefined;
  /**
   * When the chat was created.
   */
  createdAt: Date;
  /**
   * The ID of the external user who created the chat
   */
  externalUserId?: string | undefined;
  /**
   * The generation that this response's messages belong to. A generation is an immutable snapshot of the transcript; a new one is opened on compaction or message edits, while normal turns append to the current one.
   */
  generation: number;
  /**
   * Whether newer messages exist after the last message in this page (within the returned generation). Load them with an `after_seq` cursor.
   */
  hasMoreAfter: boolean;
  /**
   * Whether older messages exist before the first message in this page (within the returned generation). Load them with a `before_seq` cursor.
   */
  hasMoreBefore: boolean;
  /**
   * The ID of the chat
   */
  id: string;
  /**
   * When the last message in the chat was created.
   */
  lastMessageTimestamp: Date;
  /**
   * Present only when `query` was requested: contiguous runs of returned messages, each spanning one or more query matches and their surrounding context. Use each segment's cursors to expand it.
   */
  matchSegments?: Array<RiskSegment> | undefined;
  /**
   * The highest generation number present for this chat. To load the full history, walk from `max_generation` down to 0, requesting each generation in turn.
   */
  maxGeneration: number;
  /**
   * The list of messages in the chat for the returned generation, ordered oldest to newest by `seq`.
   */
  messages: Array<ChatMessage>;
  /**
   * The number of messages in the chat
   */
  numMessages: number;
  /**
   * Number of risk findings recorded against messages in this chat (project-scoped, found=true). Only populated by endpoints that join risk data; absent elsewhere.
   */
  riskFindingsCount?: number | undefined;
  /**
   * Present only when `risk_only` was requested: contiguous runs of returned messages, each spanning a risk finding and its surrounding context. Use each segment's cursors to expand it.
   */
  riskSegments?: Array<RiskSegment> | undefined;
  /**
   * The source of the chat: Elements, Playground, ClaudeCode (inferred from messages)
   */
  source?: string | undefined;
  /**
   * The title of the chat
   */
  title: string;
  /**
   * Total cost in USD for this chat
   */
  totalCost?: number | undefined;
  /**
   * Total input tokens used in this chat
   */
  totalInputTokens?: number | undefined;
  /**
   * Total output tokens used in this chat
   */
  totalOutputTokens?: number | undefined;
  /**
   * Total tokens (input + output) used in this chat
   */
  totalTokens?: number | undefined;
  /**
   * Trace-entry counts across the entire returned generation, independent of pagination. Each message maps to exactly one entry: a message carrying tool calls counts as a tool call regardless of role, otherwise the role decides.
   */
  totals?: ChatTotals | undefined;
  /**
   * When the chat was last updated.
   */
  updatedAt: Date;
  /**
   * The ID of the user who created the chat
   */
  userId?: string | undefined;
};
/** @internal */
export declare const Chat$inboundSchema: z.ZodMiniType<Chat, unknown>;
export declare function chatFromJSON(
  jsonString: string,
): SafeParseResult<Chat, SDKValidationError>;
//# sourceMappingURL=chat.d.ts.map
