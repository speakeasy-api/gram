import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MarketplaceSettingsResult } from "../models/components/marketplacesettingsresult.js";
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
  GetMarketplaceSettingsRequest,
  GetMarketplaceSettingsSecurity,
} from "../models/operations/getmarketplacesettings.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getMarketplaceSettings plugins
 *
 * @remarks
 * Get the marketplace settings for the current project, including the effective marketplace name and the server-side default.
 */
export declare function pluginsGetMarketplaceSettings(
  client: GramCore,
  request?: GetMarketplaceSettingsRequest | undefined,
  security?: GetMarketplaceSettingsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    MarketplaceSettingsResult,
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
//# sourceMappingURL=pluginsGetMarketplaceSettings.d.ts.map
