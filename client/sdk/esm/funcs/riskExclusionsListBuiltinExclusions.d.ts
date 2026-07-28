import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListBuiltinExclusionsResult } from "../models/components/listbuiltinexclusionsresult.js";
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
  ListBuiltinExclusionsRequest,
  ListBuiltinExclusionsSecurity,
} from "../models/operations/listbuiltinexclusions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listBuiltinExclusions risk
 *
 * @remarks
 * List the built-in exclusion library (known-safe values suppressed before they reach exclusions), grouped by category.
 */
export declare function riskExclusionsListBuiltinExclusions(
  client: GramCore,
  request?: ListBuiltinExclusionsRequest | undefined,
  security?: ListBuiltinExclusionsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListBuiltinExclusionsResult,
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
//# sourceMappingURL=riskExclusionsListBuiltinExclusions.d.ts.map
