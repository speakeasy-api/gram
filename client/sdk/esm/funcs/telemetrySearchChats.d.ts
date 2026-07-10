import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SearchChatsResult } from "../models/components/searchchatsresult.js";
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
  SearchChatsRequest,
  SearchChatsSecurity,
} from "../models/operations/searchchats.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * searchChats telemetry
 *
 * @remarks
 * Search and list chat session summaries that match a search filter
 */
export declare function telemetrySearchChats(
  client: GramCore,
  request: SearchChatsRequest,
  security?: SearchChatsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    SearchChatsResult,
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
//# sourceMappingURL=telemetrySearchChats.d.ts.map
