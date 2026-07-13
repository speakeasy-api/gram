import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Toolset } from "../models/components/toolset.js";
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
  AddOAuthProxyServerRequest,
  AddOAuthProxyServerSecurity,
} from "../models/operations/addoauthproxyserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * addOAuthProxyServer toolsets
 *
 * @remarks
 * Associate an OAuth proxy server with a toolset (admin only)
 */
export declare function toolsetsAddOAuthProxyServer(
  client: GramCore,
  request: AddOAuthProxyServerRequest,
  security?: AddOAuthProxyServerSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Toolset,
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
//# sourceMappingURL=toolsetsAddOAuthProxyServer.d.ts.map
