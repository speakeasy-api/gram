import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskSpan = {
    /**
     * End byte position within the message content.
     */
    endPos?: number | undefined;
    /**
     * The message field this span matched, in author-facing form (content, prompt, assistant, tool_result, or tool.name/tool.server/tool.function/tool.args). Empty for detectors that don't attribute a field (e.g. gitleaks, presidio).
     */
    field?: string | undefined;
    /**
     * The matched secret or sensitive data for this span.
     */
    match: string;
    /**
     * The JSON sub-path within the field for a `.get(...)` match (e.g. 'command', 'payload.sql'). Empty when the whole field value matched.
     */
    path?: string | undefined;
    /**
     * Start byte position within the message content.
     */
    startPos?: number | undefined;
};
/** @internal */
export declare const RiskSpan$inboundSchema: z.ZodMiniType<RiskSpan, unknown>;
export declare function riskSpanFromJSON(jsonString: string): SafeParseResult<RiskSpan, SDKValidationError>;
//# sourceMappingURL=riskspan.d.ts.map