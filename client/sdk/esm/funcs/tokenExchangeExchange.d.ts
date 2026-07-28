import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * exchange tokenExchange
 *
 * @remarks
 * Exchange the org-scoped device-agent install credential plus a vouched user email for a long-lived, per-user API key carrying the 'agent_user' scope. Authenticated with an org-scoped API key carrying the 'agent' scope — deliberately broader than the 'agent_user' scope the minted per-user keys carry, so a leaked per-user key cannot re-enter this endpoint to forge another user's key. The raw key is returned exactly once.
 */
export declare function tokenExchangeExchange(
  client: GramCore,
  request: operations.TokenExchangeRequest,
  security?: operations.TokenExchangeSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    components.TokenResult,
    | errors.ServiceError
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
//# sourceMappingURL=tokenExchangeExchange.d.ts.map
