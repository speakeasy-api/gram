import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { VerifyOnboardingHooksSetupResult } from "../models/components/verifyonboardinghookssetupresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { VerifyOnboardingHooksSetupRequest, VerifyOnboardingHooksSetupSecurity } from "../models/operations/verifyonboardinghookssetup.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * verifyOnboardingHooksSetup organizations
 *
 * @remarks
 * Return recent hook events for the active organization so the onboarding wizard can confirm that Claude Code, Cursor, or Codex instrumentation is delivering events to Gram. Polled from the confirm-traffic step.
 */
export declare function organizationsVerifyOnboardingHooksSetup(client: GramCore, request?: VerifyOnboardingHooksSetupRequest | undefined, security?: VerifyOnboardingHooksSetupSecurity | undefined, options?: RequestOptions): APIPromise<Result<VerifyOnboardingHooksSetupResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=organizationsVerifyOnboardingHooksSetup.d.ts.map