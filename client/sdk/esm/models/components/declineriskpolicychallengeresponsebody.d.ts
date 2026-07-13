import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DeclineRiskPolicyChallengeResponseBody = {
    /**
     * Whether the challenge is now declined.
     */
    declined: boolean;
};
/** @internal */
export declare const DeclineRiskPolicyChallengeResponseBody$inboundSchema: z.ZodMiniType<DeclineRiskPolicyChallengeResponseBody, unknown>;
export declare function declineRiskPolicyChallengeResponseBodyFromJSON(jsonString: string): SafeParseResult<DeclineRiskPolicyChallengeResponseBody, SDKValidationError>;
//# sourceMappingURL=declineriskpolicychallengeresponsebody.d.ts.map