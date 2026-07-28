import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SendEnterpriseAdminOnboardingEmailResult } from "../models/components/sendenterpriseadminonboardingemailresult.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  SendEnterpriseAdminOnboardingEmailRequest,
  SendEnterpriseAdminOnboardingEmailSecurity,
} from "../models/operations/sendenterpriseadminonboardingemail.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * sendEnterpriseAdminOnboardingEmail organizations
 *
 * @remarks
 * Send the enterprise admin onboarding email to one or more recipients. The email links each recipient to the wizard for the active organization. Used by the Platform Admin onboarding tools.
 */
export declare function organizationsSendEnterpriseAdminOnboardingEmail(
  client: GramCore,
  request: SendEnterpriseAdminOnboardingEmailRequest,
  security?: SendEnterpriseAdminOnboardingEmailSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    SendEnterpriseAdminOnboardingEmailResult,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=organizationsSendEnterpriseAdminOnboardingEmail.d.ts.map
