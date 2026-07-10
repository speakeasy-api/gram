import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListPackagesResult } from "../models/components/listpackagesresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListPackagesRequest, ListPackagesSecurity } from "../models/operations/listpackages.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listPackages packages
 *
 * @remarks
 * List all packages for a project.
 */
export declare function packagesList(client: GramCore, request?: ListPackagesRequest | undefined, security?: ListPackagesSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListPackagesResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=packagesList.d.ts.map