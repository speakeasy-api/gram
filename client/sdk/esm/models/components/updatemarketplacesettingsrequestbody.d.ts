import * as z from "zod/v4-mini";
export type UpdateMarketplaceSettingsRequestBody = {
  /**
   * Override for the marketplace name (the identifier users type as `<plugin>@<marketplace>`). Pass an empty string or omit to clear the override and fall back to the default.
   */
  marketplaceName?: string | undefined;
};
/** @internal */
export type UpdateMarketplaceSettingsRequestBody$Outbound = {
  marketplace_name?: string | undefined;
};
/** @internal */
export declare const UpdateMarketplaceSettingsRequestBody$outboundSchema: z.ZodMiniType<
  UpdateMarketplaceSettingsRequestBody$Outbound,
  UpdateMarketplaceSettingsRequestBody
>;
export declare function updateMarketplaceSettingsRequestBodyToJSON(
  updateMarketplaceSettingsRequestBody: UpdateMarketplaceSettingsRequestBody,
): string;
//# sourceMappingURL=updatemarketplacesettingsrequestbody.d.ts.map
