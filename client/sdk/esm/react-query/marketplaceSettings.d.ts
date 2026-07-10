import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetMarketplaceSettingsRequest, GetMarketplaceSettingsSecurity } from "../models/operations/getmarketplacesettings.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildMarketplaceSettingsQuery, MarketplaceSettingsQueryData, prefetchMarketplaceSettings, queryKeyMarketplaceSettings } from "./marketplaceSettings.core.js";
export { buildMarketplaceSettingsQuery, type MarketplaceSettingsQueryData, prefetchMarketplaceSettings, queryKeyMarketplaceSettings, };
export type MarketplaceSettingsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getMarketplaceSettings plugins
 *
 * @remarks
 * Get the marketplace settings for the current project, including the effective marketplace name and the server-side default.
 */
export declare function useMarketplaceSettings(request?: GetMarketplaceSettingsRequest | undefined, security?: GetMarketplaceSettingsSecurity | undefined, options?: QueryHookOptions<MarketplaceSettingsQueryData, MarketplaceSettingsQueryError>): UseQueryResult<MarketplaceSettingsQueryData, MarketplaceSettingsQueryError>;
/**
 * getMarketplaceSettings plugins
 *
 * @remarks
 * Get the marketplace settings for the current project, including the effective marketplace name and the server-side default.
 */
export declare function useMarketplaceSettingsSuspense(request?: GetMarketplaceSettingsRequest | undefined, security?: GetMarketplaceSettingsSecurity | undefined, options?: SuspenseQueryHookOptions<MarketplaceSettingsQueryData, MarketplaceSettingsQueryError>): UseSuspenseQueryResult<MarketplaceSettingsQueryData, MarketplaceSettingsQueryError>;
export declare function setMarketplaceSettingsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: MarketplaceSettingsQueryData): MarketplaceSettingsQueryData | undefined;
export declare function invalidateMarketplaceSettings(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllMarketplaceSettings(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=marketplaceSettings.d.ts.map