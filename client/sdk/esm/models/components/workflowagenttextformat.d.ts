import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Text format type
 */
export type WorkflowAgentTextFormat = {
    /**
     * The type of text format (e.g., 'text')
     */
    type: string;
};
/** @internal */
export declare const WorkflowAgentTextFormat$inboundSchema: z.ZodMiniType<WorkflowAgentTextFormat, unknown>;
export declare function workflowAgentTextFormatFromJSON(jsonString: string): SafeParseResult<WorkflowAgentTextFormat, SDKValidationError>;
//# sourceMappingURL=workflowagenttextformat.d.ts.map