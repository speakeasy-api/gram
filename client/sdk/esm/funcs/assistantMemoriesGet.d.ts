import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AssistantMemory } from "../models/components/assistantmemory.js";
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
  GetAssistantMemoryRequest,
  GetAssistantMemorySecurity,
} from "../models/operations/getassistantmemory.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getAssistantMemory assistantMemories
 *
 * @remarks
 * Get an assistant memory by ID.
 */
export declare function assistantMemoriesGet(
  client: GramCore,
  request: GetAssistantMemoryRequest,
  security?: GetAssistantMemorySecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    AssistantMemory,
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
//# sourceMappingURL=assistantMemoriesGet.d.ts.map
