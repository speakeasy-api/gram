import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListToolsetsResult } from "../models/components/listtoolsetsresult.js";
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
  ListToolsetsRequest,
  ListToolsetsSecurity,
} from "../models/operations/listtoolsets.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listToolsets toolsets
 *
 * @remarks
 * List all toolsets for a project
 */
export declare function toolsetsList(
  client: GramCore,
  request?: ListToolsetsRequest | undefined,
  security?: ListToolsetsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListToolsetsResult,
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
//# sourceMappingURL=toolsetsList.d.ts.map
