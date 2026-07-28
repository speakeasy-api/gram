import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreatePortalSessionResult } from "../models/components/createportalsessionresult.js";
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
  CreatePortalSessionRequest,
  CreatePortalSessionSecurity,
} from "../models/operations/createportalsession.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createPortalSession organizations
 *
 * @remarks
 * Create a webhook portal session.
 */
export declare function organizationsCreatePortalSession(
  client: GramCore,
  request?: CreatePortalSessionRequest | undefined,
  security?: CreatePortalSessionSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    CreatePortalSessionResult,
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
//# sourceMappingURL=organizationsCreatePortalSession.d.ts.map
