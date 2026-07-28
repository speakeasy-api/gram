import * as z from "zod/v4-mini";
export type CreateSlackAppRequestBody = {
  /**
   * Asset ID for the app icon
   */
  iconAssetId?: string | undefined;
  /**
   * Display name for the Slack app
   */
  name: string;
  /**
   * System prompt for the Slack app
   */
  systemPrompt?: string | undefined;
  /**
   * Toolset IDs to attach to this app
   */
  toolsetIds: Array<string>;
};
/** @internal */
export type CreateSlackAppRequestBody$Outbound = {
  icon_asset_id?: string | undefined;
  name: string;
  system_prompt?: string | undefined;
  toolset_ids: Array<string>;
};
/** @internal */
export declare const CreateSlackAppRequestBody$outboundSchema: z.ZodMiniType<
  CreateSlackAppRequestBody$Outbound,
  CreateSlackAppRequestBody
>;
export declare function createSlackAppRequestBodyToJSON(
  createSlackAppRequestBody: CreateSlackAppRequestBody,
): string;
//# sourceMappingURL=createslackapprequestbody.d.ts.map
