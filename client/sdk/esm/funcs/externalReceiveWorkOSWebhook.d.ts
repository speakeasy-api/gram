import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
import { ReceiveWorkOSWebhookRequest } from "../models/operations/receiveworkoswebhook.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * receiveWorkOSWebhook external
 *
 * @remarks
 * Receive and enqueue a WorkOS webhook event.
 */
export declare function externalReceiveWorkOSWebhook(
  client: GramCore,
  request?: ReceiveWorkOSWebhookRequest | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
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
//# sourceMappingURL=externalReceiveWorkOSWebhook.d.ts.map
