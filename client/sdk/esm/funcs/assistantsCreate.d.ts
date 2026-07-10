import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateAssistantRequest, CreateAssistantSecurity } from "../models/operations/createassistant.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createAssistant assistants
 *
 * @remarks
 * Create an assistant.
 */
export declare function assistantsCreate(client: GramCore, request: CreateAssistantRequest, security?: CreateAssistantSecurity | undefined, options?: RequestOptions): APIPromise<Result<Assistant, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=assistantsCreate.d.ts.map