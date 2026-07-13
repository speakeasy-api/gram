import * as z from "zod/v4-mini";
export type UpdateSlackAppRequestBody = {
  /**
   * Asset ID for the app icon
   */
  iconAssetId?: string | undefined;
  /**
   * The Slack app ID
   */
  id: string;
  /**
   * New display name for the Slack app
   */
  name?: string | undefined;
  /**
   * System prompt for the Slack app
   */
  systemPrompt?: string | undefined;
};
/** @internal */
export type UpdateSlackAppRequestBody$Outbound = {
  icon_asset_id?: string | undefined;
  id: string;
  name?: string | undefined;
  system_prompt?: string | undefined;
};
/** @internal */
export declare const UpdateSlackAppRequestBody$outboundSchema: z.ZodMiniType<
  UpdateSlackAppRequestBody$Outbound,
  UpdateSlackAppRequestBody
>;
export declare function updateSlackAppRequestBodyToJSON(
  updateSlackAppRequestBody: UpdateSlackAppRequestBody,
): string;
//# sourceMappingURL=updateslackapprequestbody.d.ts.map
