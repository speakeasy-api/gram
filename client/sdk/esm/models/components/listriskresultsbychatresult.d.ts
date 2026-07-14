import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskChatSummary } from "./riskchatsummary.js";
export type ListRiskResultsByChatResult = {
  /**
   * Risk results grouped by chat.
   */
  chats: Array<RiskChatSummary>;
  /**
   * Cursor for the next page of results.
   */
  nextCursor?: string | undefined;
};
/** @internal */
export declare const ListRiskResultsByChatResult$inboundSchema: z.ZodMiniType<
  ListRiskResultsByChatResult,
  unknown
>;
export declare function listRiskResultsByChatResultFromJSON(
  jsonString: string,
): SafeParseResult<ListRiskResultsByChatResult, SDKValidationError>;
//# sourceMappingURL=listriskresultsbychatresult.d.ts.map
