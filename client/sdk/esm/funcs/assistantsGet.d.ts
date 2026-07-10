import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetAssistantRequest, GetAssistantSecurity } from "../models/operations/getassistant.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getAssistant assistants
 *
 * @remarks
 * Get an assistant by ID.
 */
export declare function assistantsGet(client: GramCore, request: GetAssistantRequest, security?: GetAssistantSecurity | undefined, options?: RequestOptions): APIPromise<Result<Assistant, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=assistantsGet.d.ts.map