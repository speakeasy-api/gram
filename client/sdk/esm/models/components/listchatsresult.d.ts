import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ChatOverview } from "./chatoverview.js";
export type ListChatsResult = {
    /**
     * The list of chats
     */
    chats: Array<ChatOverview>;
    /**
     * Total number of chats (before pagination)
     */
    total: number;
};
/** @internal */
export declare const ListChatsResult$inboundSchema: z.ZodMiniType<ListChatsResult, unknown>;
export declare function listChatsResultFromJSON(jsonString: string): SafeParseResult<ListChatsResult, SDKValidationError>;
//# sourceMappingURL=listchatsresult.d.ts.map