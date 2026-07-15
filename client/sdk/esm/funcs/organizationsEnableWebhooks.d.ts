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
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  EnableWebhooksRequest,
  EnableWebhooksSecurity,
} from "../models/operations/enablewebhooks.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * enableWebhooks organizations
 *
 * @remarks
 * Enable  webhooks for the active organization.
 */
export declare function organizationsEnableWebhooks(
  client: GramCore,
  request?: EnableWebhooksRequest | undefined,
  security?: EnableWebhooksSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
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
//# sourceMappingURL=organizationsEnableWebhooks.d.ts.map
