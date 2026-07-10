import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAssetsResult } from "../models/components/listassetsresult.js";
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
  ListAssetsRequest,
  ListAssetsSecurity,
} from "../models/operations/listassets.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listAssets assets
 *
 * @remarks
 * List all assets for a project.
 */
export declare function assetsListAssets(
  client: GramCore,
  request?: ListAssetsRequest | undefined,
  security?: ListAssetsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListAssetsResult,
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
//# sourceMappingURL=assetsListAssets.d.ts.map
