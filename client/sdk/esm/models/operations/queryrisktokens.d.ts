import * as z from "zod/v4-mini";
import { TelemetryWindowPayload, TelemetryWindowPayload$Outbound } from "../components/telemetrywindowpayload.js";
export type QueryRiskTokensSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type QueryRiskTokensRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    telemetryWindowPayload: TelemetryWindowPayload;
};
/** @internal */
export type QueryRiskTokensSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const QueryRiskTokensSecurity$outboundSchema: z.ZodMiniType<QueryRiskTokensSecurity$Outbound, QueryRiskTokensSecurity>;
export declare function queryRiskTokensSecurityToJSON(queryRiskTokensSecurity: QueryRiskTokensSecurity): string;
/** @internal */
export type QueryRiskTokensRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    TelemetryWindowPayload: TelemetryWindowPayload$Outbound;
};
/** @internal */
export declare const QueryRiskTokensRequest$outboundSchema: z.ZodMiniType<QueryRiskTokensRequest$Outbound, QueryRiskTokensRequest>;
export declare function queryRiskTokensRequestToJSON(queryRiskTokensRequest: QueryRiskTokensRequest): string;
//# sourceMappingURL=queryrisktokens.d.ts.map