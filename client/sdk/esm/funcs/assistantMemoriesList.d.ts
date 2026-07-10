import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAssistantMemoriesRequest, ListAssistantMemoriesResponse, ListAssistantMemoriesSecurity } from "../models/operations/listassistantmemories.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
import { PageIterator } from "../types/operations.js";
/**
 * listAssistantMemories assistantMemories
 *
 * @remarks
 * List assistant memories for an assistant.
 */
export declare function assistantMemoriesList(client: GramCore, request: ListAssistantMemoriesRequest, security?: ListAssistantMemoriesSecurity | undefined, options?: RequestOptions): APIPromise<PageIterator<Result<ListAssistantMemoriesResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>, {
    cursor: string;
}>>;
//# sourceMappingURL=assistantMemoriesList.d.ts.map