import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listMessages assistants
 *
 * @remarks
 * List a dashboard conversation log for a chat (the user's messages and the assistant's delivered replies). Only the user who owns the conversation may read it. Poll with after_seq to fetch only newer messages.
 */
export declare function assistantsListMessages(client: GramCore, request: operations.ListAssistantMessagesRequest, security?: operations.ListAssistantMessagesSecurity | undefined, options?: RequestOptions): APIPromise<Result<components.ListMessagesResult, errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=assistantsListMessages.d.ts.map