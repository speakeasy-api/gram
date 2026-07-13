import * as z from "zod/v4-mini";
import { TelemetryWindowPayload, TelemetryWindowPayload$Outbound } from "../components/telemetrywindowpayload.js";
export type QueryTumDetailsSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type QueryTumDetailsRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    telemetryWindowPayload: TelemetryWindowPayload;
};
/** @internal */
export type QueryTumDetailsSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const QueryTumDetailsSecurity$outboundSchema: z.ZodMiniType<QueryTumDetailsSecurity$Outbound, QueryTumDetailsSecurity>;
export declare function queryTumDetailsSecurityToJSON(queryTumDetailsSecurity: QueryTumDetailsSecurity): string;
/** @internal */
export type QueryTumDetailsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    TelemetryWindowPayload: TelemetryWindowPayload$Outbound;
};
/** @internal */
export declare const QueryTumDetailsRequest$outboundSchema: z.ZodMiniType<QueryTumDetailsRequest$Outbound, QueryTumDetailsRequest>;
export declare function queryTumDetailsRequestToJSON(queryTumDetailsRequest: QueryTumDetailsRequest): string;
//# sourceMappingURL=querytumdetails.d.ts.map