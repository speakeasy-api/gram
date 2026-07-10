import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SlackAppResult = {
  createdAt: Date;
  /**
   * Asset ID for the app icon
   */
  iconAssetId?: string | undefined;
  /**
   * The Slack app ID
   */
  id: string;
  /**
   * Display name of the Slack app
   */
  name: string;
  /**
   * OAuth callback URL for this app
   */
  redirectUrl?: string | undefined;
  /**
   * Event subscription URL for this app
   */
  requestUrl?: string | undefined;
  /**
   * The Slack app Client ID
   */
  slackClientId?: string | undefined;
  /**
   * The connected Slack workspace ID
   */
  slackTeamId?: string | undefined;
  /**
   * The connected Slack workspace name
   */
  slackTeamName?: string | undefined;
  /**
   * Current status: unconfigured, active
   */
  status: string;
  /**
   * System prompt for the Slack app
   */
  systemPrompt?: string | undefined;
  /**
   * Attached toolset IDs
   */
  toolsetIds: Array<string>;
  updatedAt: Date;
};
/** @internal */
export declare const SlackAppResult$inboundSchema: z.ZodMiniType<
  SlackAppResult,
  unknown
>;
export declare function slackAppResultFromJSON(
  jsonString: string,
): SafeParseResult<SlackAppResult, SDKValidationError>;
//# sourceMappingURL=slackappresult.d.ts.map
