import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAllowedOriginsResult } from "../models/components/listallowedoriginsresult.js";
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
  ListAllowedOriginsRequest,
  ListAllowedOriginsSecurity,
} from "../models/operations/listallowedorigins.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listAllowedOrigins projects
 *
 * @remarks
 * List allowed origins for a project.
 */
export declare function projectsListAllowedOrigins(
  client: GramCore,
  request?: ListAllowedOriginsRequest | undefined,
  security?: ListAllowedOriginsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListAllowedOriginsResult,
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
//# sourceMappingURL=projectsListAllowedOrigins.d.ts.map
