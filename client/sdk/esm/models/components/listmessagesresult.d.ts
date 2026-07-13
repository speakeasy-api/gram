import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { DashboardMessage } from "./dashboardmessage.js";
export type ListMessagesResult = {
    /**
     * Conversation log in send order.
     */
    messages: Array<DashboardMessage>;
};
/** @internal */
export declare const ListMessagesResult$inboundSchema: z.ZodMiniType<ListMessagesResult, unknown>;
export declare function listMessagesResultFromJSON(jsonString: string): SafeParseResult<ListMessagesResult, SDKValidationError>;
//# sourceMappingURL=listmessagesresult.d.ts.map