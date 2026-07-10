import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TestDetectionRuleMatch } from "./testdetectionrulematch.js";
export type TestDetectionRuleResult = {
    /**
     * Matches the rule found in the sample.
     */
    matches: Array<TestDetectionRuleMatch>;
    /**
     * Why the rule isn't supported when `supported` is false.
     */
    reason?: string | undefined;
    /**
     * False when the rule has no text-only detector (e.g. `shadow_mcp`, `destructive_tool`).
     */
    supported: boolean;
};
/** @internal */
export declare const TestDetectionRuleResult$inboundSchema: z.ZodMiniType<TestDetectionRuleResult, unknown>;
export declare function testDetectionRuleResultFromJSON(jsonString: string): SafeParseResult<TestDetectionRuleResult, SDKValidationError>;
//# sourceMappingURL=testdetectionruleresult.d.ts.map