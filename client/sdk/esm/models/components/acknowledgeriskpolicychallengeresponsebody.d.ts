import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AcknowledgeRiskPolicyChallengeResponseBody = {
    /**
     * Whether the challenge is now acknowledged.
     */
    acknowledged: boolean;
    /**
     * RFC3339 time until which the acknowledgement suppresses re-challenge.
     */
    expiresAt?: Date | undefined;
    /**
     * The policy that issued the warning.
     */
    policyName?: string | undefined;
};
/** @internal */
export declare const AcknowledgeRiskPolicyChallengeResponseBody$inboundSchema: z.ZodMiniType<AcknowledgeRiskPolicyChallengeResponseBody, unknown>;
export declare function acknowledgeRiskPolicyChallengeResponseBodyFromJSON(jsonString: string): SafeParseResult<AcknowledgeRiskPolicyChallengeResponseBody, SDKValidationError>;
//# sourceMappingURL=acknowledgeriskpolicychallengeresponsebody.d.ts.map