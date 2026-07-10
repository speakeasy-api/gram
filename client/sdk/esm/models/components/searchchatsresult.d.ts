import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ChatSummary } from "./chatsummary.js";
/**
 * Result of searching chat session summaries
 */
export type SearchChatsResult = {
    /**
     * List of chat session summaries
     */
    chats: Array<ChatSummary>;
    /**
     * Cursor for next page
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const SearchChatsResult$inboundSchema: z.ZodMiniType<SearchChatsResult, unknown>;
export declare function searchChatsResultFromJSON(jsonString: string): SafeParseResult<SearchChatsResult, SDKValidationError>;
//# sourceMappingURL=searchchatsresult.d.ts.map