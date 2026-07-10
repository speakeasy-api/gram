import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolsetEntry } from "./toolsetentry.js";
export type ListToolsetsResult = {
    /**
     * The list of toolsets
     */
    toolsets: Array<ToolsetEntry>;
};
/** @internal */
export declare const ListToolsetsResult$inboundSchema: z.ZodMiniType<ListToolsetsResult, unknown>;
export declare function listToolsetsResultFromJSON(jsonString: string): SafeParseResult<ListToolsetsResult, SDKValidationError>;
//# sourceMappingURL=listtoolsetsresult.d.ts.map