import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { MarketplaceSettingsResult } from "./marketplacesettingsresult.js";
export type UpdateMarketplaceSettingsResult = {
  /**
   * True when the new name reached the MCP plugins and marketplace manifests but the observability (hooks) plugin could not be updated yet because the organization is not approved for the latest hooks version; it will update automatically once the organization is rolled forward.
   */
  hooksUpdateDeferred?: boolean | undefined;
  /**
   * Whether the marketplace was automatically republished to GitHub as part of this update.
   */
  republished: boolean;
  settings: MarketplaceSettingsResult;
};
/** @internal */
export declare const UpdateMarketplaceSettingsResult$inboundSchema: z.ZodMiniType<
  UpdateMarketplaceSettingsResult,
  unknown
>;
export declare function updateMarketplaceSettingsResultFromJSON(
  jsonString: string,
): SafeParseResult<UpdateMarketplaceSettingsResult, SDKValidationError>;
//# sourceMappingURL=updatemarketplacesettingsresult.d.ts.map
