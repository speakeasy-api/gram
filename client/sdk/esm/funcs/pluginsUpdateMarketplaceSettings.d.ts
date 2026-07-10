import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UpdateMarketplaceSettingsResult } from "../models/components/updatemarketplacesettingsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateMarketplaceSettingsRequest, UpdateMarketplaceSettingsSecurity } from "../models/operations/updatemarketplacesettings.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateMarketplaceSettings plugins
 *
 * @remarks
 * Update the marketplace settings for the current project. If a marketplace is already published, the updated settings are pushed to GitHub before the call returns.
 */
export declare function pluginsUpdateMarketplaceSettings(client: GramCore, request: UpdateMarketplaceSettingsRequest, security?: UpdateMarketplaceSettingsSecurity | undefined, options?: RequestOptions): APIPromise<Result<UpdateMarketplaceSettingsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=pluginsUpdateMarketplaceSettings.d.ts.map