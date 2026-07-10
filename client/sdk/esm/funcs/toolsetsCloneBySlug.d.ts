import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CloneToolsetRequest, CloneToolsetSecurity } from "../models/operations/clonetoolset.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * cloneToolset toolsets
 *
 * @remarks
 * Clone an existing toolset with a new name
 */
export declare function toolsetsCloneBySlug(client: GramCore, request: CloneToolsetRequest, security?: CloneToolsetSecurity | undefined, options?: RequestOptions): APIPromise<Result<Toolset, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=toolsetsCloneBySlug.d.ts.map