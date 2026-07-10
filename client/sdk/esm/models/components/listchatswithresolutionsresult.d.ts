import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ChatOverviewWithResolutions } from "./chatoverviewwithresolutions.js";
/**
 * Result of listing chats with resolutions
 */
export type ListChatsWithResolutionsResult = {
    /**
     * List of chats with resolutions
     */
    chats: Array<ChatOverviewWithResolutions>;
    /**
     * Total number of chats (before pagination)
     */
    total: number;
};
/** @internal */
export declare const ListChatsWithResolutionsResult$inboundSchema: z.ZodMiniType<ListChatsWithResolutionsResult, unknown>;
export declare function listChatsWithResolutionsResultFromJSON(jsonString: string): SafeParseResult<ListChatsWithResolutionsResult, SDKValidationError>;
//# sourceMappingURL=listchatswithresolutionsresult.d.ts.map