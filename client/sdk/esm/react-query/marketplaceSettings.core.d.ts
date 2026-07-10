import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { MarketplaceSettingsResult } from "../models/components/marketplacesettingsresult.js";
import { GetMarketplaceSettingsRequest, GetMarketplaceSettingsSecurity } from "../models/operations/getmarketplacesettings.js";
export type MarketplaceSettingsQueryData = MarketplaceSettingsResult;
export declare function prefetchMarketplaceSettings(queryClient: QueryClient, client$: GramCore, request?: GetMarketplaceSettingsRequest | undefined, security?: GetMarketplaceSettingsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildMarketplaceSettingsQuery(client$: GramCore, request?: GetMarketplaceSettingsRequest | undefined, security?: GetMarketplaceSettingsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<MarketplaceSettingsQueryData>;
};
export declare function queryKeyMarketplaceSettings(parameters: {
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=marketplaceSettings.core.d.ts.map