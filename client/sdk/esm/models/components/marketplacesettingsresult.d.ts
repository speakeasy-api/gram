import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type MarketplaceSettingsResult = {
    /**
     * The default marketplace name used when no override is configured.
     */
    defaultName: string;
    /**
     * The marketplace name that will be used at publish time (override if set, otherwise default).
     */
    effectiveName: string;
    /**
     * User-provided override for the marketplace name. Absent when no override is configured.
     */
    marketplaceName?: string | undefined;
};
/** @internal */
export declare const MarketplaceSettingsResult$inboundSchema: z.ZodMiniType<MarketplaceSettingsResult, unknown>;
export declare function marketplaceSettingsResultFromJSON(jsonString: string): SafeParseResult<MarketplaceSettingsResult, SDKValidationError>;
//# sourceMappingURL=marketplacesettingsresult.d.ts.map