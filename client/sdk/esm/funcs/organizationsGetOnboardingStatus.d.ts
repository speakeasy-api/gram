import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OnboardingStatusResult } from "../models/components/onboardingstatusresult.js";
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
  GetOnboardingStatusRequest,
  GetOnboardingStatusSecurity,
} from "../models/operations/getonboardingstatus.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getOnboardingStatus organizations
 *
 * @remarks
 * Get the onboarding status for the active organization by checking WorkOS SSO connections and directory sync state.
 */
export declare function organizationsGetOnboardingStatus(
  client: GramCore,
  request?: GetOnboardingStatusRequest | undefined,
  security?: GetOnboardingStatusSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    OnboardingStatusResult,
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
//# sourceMappingURL=organizationsGetOnboardingStatus.d.ts.map
