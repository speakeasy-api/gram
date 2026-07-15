import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskTokensPoint } from "./risktokenspoint.js";
/**
 * Result of the token-by-risk breakdown query
 */
export type QueryRiskTokensResult = {
    /**
     * Timeseries bucket width in seconds. Always 86400 — the source aggregate is bucketed daily.
     */
    intervalSeconds: number;
    /**
     * Gap-filled daily buckets in ascending time order
     */
    points: Array<RiskTokensPoint>;
};
/** @internal */
export declare const QueryRiskTokensResult$inboundSchema: z.ZodMiniType<QueryRiskTokensResult, unknown>;
export declare function queryRiskTokensResultFromJSON(jsonString: string): SafeParseResult<QueryRiskTokensResult, SDKValidationError>;
//# sourceMappingURL=queryrisktokensresult.d.ts.map