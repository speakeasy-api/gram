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
  ClearMCPRegistryCacheRequest,
  ClearMCPRegistryCacheSecurity,
} from "../models/operations/clearmcpregistrycache.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * clearCache mcpRegistries
 *
 * @remarks
 * Clear the registry cache for a specific registry (admin only)
 */
export declare function mcpRegistriesClearCache(
  client: GramCore,
  request: ClearMCPRegistryCacheRequest,
  security?: ClearMCPRegistryCacheSecurity | undefined,
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
//# sourceMappingURL=mcpRegistriesClearCache.d.ts.map
