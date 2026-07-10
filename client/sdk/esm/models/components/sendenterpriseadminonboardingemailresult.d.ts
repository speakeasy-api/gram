import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SendEnterpriseAdminOnboardingEmailResult = {
    /**
     * Number of recipients the email was dispatched to.
     */
    sentCount: number;
    /**
     * The setup link embedded in the dispatched email.
     */
    setupLink: string;
};
/** @internal */
export declare const SendEnterpriseAdminOnboardingEmailResult$inboundSchema: z.ZodMiniType<SendEnterpriseAdminOnboardingEmailResult, unknown>;
export declare function sendEnterpriseAdminOnboardingEmailResultFromJSON(jsonString: string): SafeParseResult<SendEnterpriseAdminOnboardingEmailResult, SDKValidationError>;
//# sourceMappingURL=sendenterpriseadminonboardingemailresult.d.ts.map