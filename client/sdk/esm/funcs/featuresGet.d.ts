import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetProductFeaturesResponseBody } from "../models/components/getproductfeaturesresponsebody.js";
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
  GetProductFeaturesRequest,
  GetProductFeaturesSecurity,
} from "../models/operations/getproductfeatures.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getProductFeatures features
 *
 * @remarks
 * Get the current state of all product feature flags.
 */
export declare function featuresGet(
  client: GramCore,
  request?: GetProductFeaturesRequest | undefined,
  security?: GetProductFeaturesSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GetProductFeaturesResponseBody,
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
//# sourceMappingURL=featuresGet.d.ts.map
