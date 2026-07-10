import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolsetSummary } from "./toolsetsummary.js";
export type ListToolsetSummariesResult = {
    /**
     * The list of toolset summaries
     */
    toolsets: Array<ToolsetSummary>;
};
/** @internal */
export declare const ListToolsetSummariesResult$inboundSchema: z.ZodMiniType<ListToolsetSummariesResult, unknown>;
export declare function listToolsetSummariesResultFromJSON(jsonString: string): SafeParseResult<ListToolsetSummariesResult, SDKValidationError>;
//# sourceMappingURL=listtoolsetsummariesresult.d.ts.map