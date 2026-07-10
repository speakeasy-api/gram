import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadFunctionsResult } from "../models/components/uploadfunctionsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UploadFunctionsRequest, UploadFunctionsSecurity } from "../models/operations/uploadfunctions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * uploadFunctions assets
 *
 * @remarks
 * Upload functions to Gram.
 */
export declare function assetsUploadFunctions(client: GramCore, request: UploadFunctionsRequest, security?: UploadFunctionsSecurity | undefined, options?: RequestOptions): APIPromise<Result<UploadFunctionsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=assetsUploadFunctions.d.ts.map