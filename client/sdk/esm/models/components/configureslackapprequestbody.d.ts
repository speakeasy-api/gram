import * as z from "zod/v4-mini";
export type ConfigureSlackAppRequestBody = {
  /**
   * The Slack app ID
   */
  id: string;
  /**
   * Slack app Client ID
   */
  slackClientId: string;
  /**
   * Slack app Client Secret
   */
  slackClientSecret: string;
  /**
   * Slack app Signing Secret
   */
  slackSigningSecret: string;
};
/** @internal */
export type ConfigureSlackAppRequestBody$Outbound = {
  id: string;
  slack_client_id: string;
  slack_client_secret: string;
  slack_signing_secret: string;
};
/** @internal */
export declare const ConfigureSlackAppRequestBody$outboundSchema: z.ZodMiniType<
  ConfigureSlackAppRequestBody$Outbound,
  ConfigureSlackAppRequestBody
>;
export declare function configureSlackAppRequestBodyToJSON(
  configureSlackAppRequestBody: ConfigureSlackAppRequestBody,
): string;
//# sourceMappingURL=configureslackapprequestbody.d.ts.map
