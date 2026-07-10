import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SendEnterpriseAdminOnboardingEmailResult } from "../models/components/sendenterpriseadminonboardingemailresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SendEnterpriseAdminOnboardingEmailRequest, SendEnterpriseAdminOnboardingEmailSecurity } from "../models/operations/sendenterpriseadminonboardingemail.js";
import { MutationHookOptions } from "./_types.js";
export type SendEnterpriseAdminOnboardingEmailMutationVariables = {
    request: SendEnterpriseAdminOnboardingEmailRequest;
    security?: SendEnterpriseAdminOnboardingEmailSecurity | undefined;
    options?: RequestOptions;
};
export type SendEnterpriseAdminOnboardingEmailMutationData = SendEnterpriseAdminOnboardingEmailResult;
export type SendEnterpriseAdminOnboardingEmailMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * sendEnterpriseAdminOnboardingEmail organizations
 *
 * @remarks
 * Send the enterprise admin onboarding email to one or more recipients. The email links each recipient to the wizard for the active organization. Used by the Platform Admin onboarding tools.
 */
export declare function useSendEnterpriseAdminOnboardingEmailMutation(options?: MutationHookOptions<SendEnterpriseAdminOnboardingEmailMutationData, SendEnterpriseAdminOnboardingEmailMutationError, SendEnterpriseAdminOnboardingEmailMutationVariables>): UseMutationResult<SendEnterpriseAdminOnboardingEmailMutationData, SendEnterpriseAdminOnboardingEmailMutationError, SendEnterpriseAdminOnboardingEmailMutationVariables>;
export declare function mutationKeySendEnterpriseAdminOnboardingEmail(): MutationKey;
export declare function buildSendEnterpriseAdminOnboardingEmailMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: SendEnterpriseAdminOnboardingEmailMutationVariables) => Promise<SendEnterpriseAdminOnboardingEmailMutationData>;
};
//# sourceMappingURL=sendEnterpriseAdminOnboardingEmail.d.ts.map