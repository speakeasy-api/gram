import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Organization } from "../models/components/organization.js";
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
  GetOrganizationRequest,
  GetOrganizationSecurity,
} from "../models/operations/getorganization.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * get organizations
 *
 * @remarks
 * Get the active organization from the session.
 */
export declare function organizationsGet(
  client: GramCore,
  request?: GetOrganizationRequest | undefined,
  security?: GetOrganizationSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Organization,
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
//# sourceMappingURL=organizationsGet.d.ts.map
