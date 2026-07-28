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
  RemoveOAuthServerRequest,
  RemoveOAuthServerSecurity,
} from "../models/operations/removeoauthserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * removeOAuthServer toolsets
 *
 * @remarks
 * Remove OAuth server association from a toolset
 */
export declare function toolsetsRemoveOAuthServer(
  client: GramCore,
  request: RemoveOAuthServerRequest,
  security?: RemoveOAuthServerSecurity | undefined,
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
//# sourceMappingURL=toolsetsRemoveOAuthServer.d.ts.map
