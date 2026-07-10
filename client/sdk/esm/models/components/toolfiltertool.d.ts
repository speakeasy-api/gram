import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A tool referenced by a tool filter scope, identified by URN and display name.
 */
export type ToolFilterTool = {
    /**
     * The display name of the tool, with any variation rename from the resolved group applied (matching the runtime wire)
     */
    name: string;
    /**
     * The URN of the tool
     */
    toolUrn: string;
};
/** @internal */
export declare const ToolFilterTool$inboundSchema: z.ZodMiniType<ToolFilterTool, unknown>;
export declare function toolFilterToolFromJSON(jsonString: string): SafeParseResult<ToolFilterTool, SDKValidationError>;
//# sourceMappingURL=toolfiltertool.d.ts.map