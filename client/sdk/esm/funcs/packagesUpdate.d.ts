import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  UpdatePackageRequest,
  UpdatePackageResponse,
  UpdatePackageSecurity,
} from "../models/operations/updatepackage.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updatePackage packages
 *
 * @remarks
 * Update package details.
 */
export declare function packagesUpdate(
  client: GramCore,
  request: UpdatePackageRequest,
  security?: UpdatePackageSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    UpdatePackageResponse,
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
//# sourceMappingURL=packagesUpdate.d.ts.map
