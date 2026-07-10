import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolsResult } from "../models/components/listtoolsresult.js";
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
  ListToolsRequest,
  ListToolsSecurity,
} from "../models/operations/listtools.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listTools tools
 *
 * @remarks
 * List all tools for a project
 */
export declare function toolsList(
  client: GramCore,
  request?: ListToolsRequest | undefined,
  security?: ListToolsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListToolsResult,
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
//# sourceMappingURL=toolsList.d.ts.map
