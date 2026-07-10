import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListChatsResult } from "../models/components/listchatsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListChatsRequest, ListChatsSecurity } from "../models/operations/listchats.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listChats chat
 *
 * @remarks
 * List all chats for a project
 */
export declare function chatList(client: GramCore, request?: ListChatsRequest | undefined, security?: ListChatsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListChatsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=chatList.d.ts.map