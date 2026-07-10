import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ChatOverview = {
    /**
     * Email of the AI account that produced the chat, resolved from the linked AI account. May differ from the employee's work email (e.g. a personal account).
     */
    accountEmail?: string | undefined;
    /**
     * Account type that produced the chat ('team', 'personal', or empty), resolved from the linked AI account.
     */
    accountType?: string | undefined;
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
export declare const ChatOverview$inboundSchema: z.ZodMiniType<ChatOverview, unknown>;
export declare function chatOverviewFromJSON(jsonString: string): SafeParseResult<ChatOverview, SDKValidationError>;
//# sourceMappingURL=chatoverview.d.ts.map