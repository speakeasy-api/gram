import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAssistantsResult } from "../models/components/listassistantsresult.js";
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
  ListAssistantsRequest,
  ListAssistantsSecurity,
} from "../models/operations/listassistants.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listAssistants assistants
 *
 * @remarks
 * List assistants for the current project.
 */
export declare function assistantsList(
  client: GramCore,
  request?: ListAssistantsRequest | undefined,
  security?: ListAssistantsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListAssistantsResult,
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
//# sourceMappingURL=assistantsList.d.ts.map
