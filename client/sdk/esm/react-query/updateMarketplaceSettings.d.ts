import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UpdateMarketplaceSettingsResult } from "../models/components/updatemarketplacesettingsresult.js";
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
  UpdateMarketplaceSettingsRequest,
  UpdateMarketplaceSettingsSecurity,
} from "../models/operations/updatemarketplacesettings.js";
import { MutationHookOptions } from "./_types.js";
export type UpdateMarketplaceSettingsMutationVariables = {
  request: UpdateMarketplaceSettingsRequest;
  security?: UpdateMarketplaceSettingsSecurity | undefined;
  options?: RequestOptions;
};
export type UpdateMarketplaceSettingsMutationData =
  UpdateMarketplaceSettingsResult;
export type UpdateMarketplaceSettingsMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * updateMarketplaceSettings plugins
 *
 * @remarks
 * Update the marketplace settings for the current project. If a marketplace is already published, the updated settings are pushed to GitHub before the call returns.
 */
export declare function useUpdateMarketplaceSettingsMutation(
  options?: MutationHookOptions<
    UpdateMarketplaceSettingsMutationData,
    UpdateMarketplaceSettingsMutationError,
    UpdateMarketplaceSettingsMutationVariables
  >,
): UseMutationResult<
  UpdateMarketplaceSettingsMutationData,
  UpdateMarketplaceSettingsMutationError,
  UpdateMarketplaceSettingsMutationVariables
>;
export declare function mutationKeyUpdateMarketplaceSettings(): MutationKey;
export declare function buildUpdateMarketplaceSettingsMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UpdateMarketplaceSettingsMutationVariables,
  ) => Promise<UpdateMarketplaceSettingsMutationData>;
};
//# sourceMappingURL=updateMarketplaceSettings.d.ts.map
