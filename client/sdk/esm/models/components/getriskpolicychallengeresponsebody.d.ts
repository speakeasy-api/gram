import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type GetRiskPolicyChallengeResponseBody = {
    /**
     * Whether this challenge has already been acknowledged.
     */
    acknowledged: boolean;
    /**
     * RFC3339 time the acknowledgement link expires.
     */
    expiresAt?: Date | undefined;
    /**
     * Human-facing challenge message describing what was flagged.
     */
    message: string;
    /**
     * The policy that issued the warning.
     */
    policyName?: string | undefined;
    /**
     * The tool the challenge applies to, if any.
     */
    toolName?: string | undefined;
};
/** @internal */
export declare const GetRiskPolicyChallengeResponseBody$inboundSchema: z.ZodMiniType<GetRiskPolicyChallengeResponseBody, unknown>;
export declare function getRiskPolicyChallengeResponseBodyFromJSON(jsonString: string): SafeParseResult<GetRiskPolicyChallengeResponseBody, SDKValidationError>;
//# sourceMappingURL=getriskpolicychallengeresponsebody.d.ts.map