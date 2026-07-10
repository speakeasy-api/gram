import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreatePackageResult } from "../models/components/createpackageresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreatePackageRequest, CreatePackageSecurity } from "../models/operations/createpackage.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createPackage packages
 *
 * @remarks
 * Create a new package for a project.
 */
export declare function packagesCreate(client: GramCore, request: CreatePackageRequest, security?: CreatePackageSecurity | undefined, options?: RequestOptions): APIPromise<Result<CreatePackageResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=packagesCreate.d.ts.map