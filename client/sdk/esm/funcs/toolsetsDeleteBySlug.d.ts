import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteToolsetRequest, DeleteToolsetSecurity } from "../models/operations/deletetoolset.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteToolset toolsets
 *
 * @remarks
 * Delete a toolset by its ID
 */
export declare function toolsetsDeleteBySlug(client: GramCore, request: DeleteToolsetRequest, security?: DeleteToolsetSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=toolsetsDeleteBySlug.d.ts.map