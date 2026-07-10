import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListSourcesResult } from "../models/components/listsourcesresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListChatSourcesRequest, ListChatSourcesSecurity } from "../models/operations/listchatsources.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listSources chat
 *
 * @remarks
 * List the distinct agent sources present in this project's chats, for populating the agent-type filter on the Agent Sessions page.
 */
export declare function chatListSources(client: GramCore, request?: ListChatSourcesRequest | undefined, security?: ListChatSourcesSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListSourcesResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=chatListSources.d.ts.map