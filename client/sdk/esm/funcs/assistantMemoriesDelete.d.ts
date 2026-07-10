import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteAssistantMemoryRequest, DeleteAssistantMemorySecurity } from "../models/operations/deleteassistantmemory.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteAssistantMemory assistantMemories
 *
 * @remarks
 * Delete an assistant memory by ID.
 */
export declare function assistantMemoriesDelete(client: GramCore, request: DeleteAssistantMemoryRequest, security?: DeleteAssistantMemorySecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=assistantMemoriesDelete.d.ts.map