import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskResultRedacted } from "./riskresultredacted.js";
export type ListRiskResultsForAgentResult = {
    /**
     * Cursor for the next page of results.
     */
    nextCursor?: string | undefined;
    /**
     * The list of risk results with match content redacted to opaque fingerprints.
     */
    results: Array<RiskResultRedacted>;
    /**
     * Total number of findings across all enabled policies.
     */
    totalCount: number;
};
/** @internal */
export declare const ListRiskResultsForAgentResult$inboundSchema: z.ZodMiniType<ListRiskResultsForAgentResult, unknown>;
export declare function listRiskResultsForAgentResultFromJSON(jsonString: string): SafeParseResult<ListRiskResultsForAgentResult, SDKValidationError>;
//# sourceMappingURL=listriskresultsforagentresult.d.ts.map