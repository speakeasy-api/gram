import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateToolsetRequest, CreateToolsetSecurity } from "../models/operations/createtoolset.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createToolset toolsets
 *
 * @remarks
 * Create a new toolset with associated tools
 */
export declare function toolsetsCreate(client: GramCore, request: CreateToolsetRequest, security?: CreateToolsetSecurity | undefined, options?: RequestOptions): APIPromise<Result<Toolset, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=toolsetsCreate.d.ts.map