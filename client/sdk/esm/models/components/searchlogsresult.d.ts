import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TelemetryLogRecord } from "./telemetrylogrecord.js";
/**
 * Result of searching telemetry logs
 */
export type SearchLogsResult = {
    /**
     * List of telemetry log records
     */
    logs: Array<TelemetryLogRecord>;
    /**
     * Cursor for next page
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const SearchLogsResult$inboundSchema: z.ZodMiniType<SearchLogsResult, unknown>;
export declare function searchLogsResultFromJSON(jsonString: string): SafeParseResult<SearchLogsResult, SDKValidationError>;
//# sourceMappingURL=searchlogsresult.d.ts.map