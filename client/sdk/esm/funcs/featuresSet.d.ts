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
  SetProductFeatureRequest,
  SetProductFeatureSecurity,
} from "../models/operations/setproductfeature.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * setProductFeature features
 *
 * @remarks
 * Enable or disable an organization feature flag.
 */
export declare function featuresSet(
  client: GramCore,
  request: SetProductFeatureRequest,
  security?: SetProductFeatureSecurity | undefined,
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
//# sourceMappingURL=featuresSet.d.ts.map
