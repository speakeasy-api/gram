import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListVariationsResult } from "../models/components/listvariationsresult.js";
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
  ListGlobalVariationsRequest,
  ListGlobalVariationsSecurity,
} from "../models/operations/listglobalvariations.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listGlobal variations
 *
 * @remarks
 * List globally defined tool variations.
 */
export declare function variationsListGlobal(
  client: GramCore,
  request?: ListGlobalVariationsRequest | undefined,
  security?: ListGlobalVariationsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListVariationsResult,
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
//# sourceMappingURL=variationsListGlobal.d.ts.map
