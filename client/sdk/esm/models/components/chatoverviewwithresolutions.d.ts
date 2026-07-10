import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ChatResolution } from "./chatresolution.js";
/**
 * Chat overview with embedded resolution data
 */
export type ChatOverviewWithResolutions = {
  /**
   * When the chat was created.
   */
  createdAt: Date;
  /**
   * The ID of the external user who created the chat
   */
  externalUserId?: string | undefined;
  /**
   * The ID of the chat
   */
  id: string;
  /**
   * When the last message in the chat was created.
   */
  lastMessageTimestamp: Date;
  /**
   * The number of messages in the chat
   */
  numMessages: number;
  /**
   * List of resolutions for this chat
   */
  resolutions: Array<ChatResolution>;
  /**
   * Number of risk findings recorded against messages in this chat (project-scoped, found=true). Only populated by endpoints that join risk data; absent elsewhere.
   */
  riskFindingsCount?: number | undefined;
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
   * When the chat was last updated.
   */
  updatedAt: Date;
  /**
   * The ID of the user who created the chat
   */
  userId?: string | undefined;
};
/** @internal */
export declare const ChatOverviewWithResolutions$inboundSchema: z.ZodMiniType<
  ChatOverviewWithResolutions,
  unknown
>;
export declare function chatOverviewWithResolutionsFromJSON(
  jsonString: string,
): SafeParseResult<ChatOverviewWithResolutions, SDKValidationError>;
//# sourceMappingURL=chatoverviewwithresolutions.d.ts.map
