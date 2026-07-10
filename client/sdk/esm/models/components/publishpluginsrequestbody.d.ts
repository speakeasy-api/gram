import * as z from "zod/v4-mini";
export type PublishPluginsRequestBody = {
  /**
   * GitHub usernames to add as collaborators on the repo.
   */
  githubUsernames?: Array<string> | undefined;
};
/** @internal */
export type PublishPluginsRequestBody$Outbound = {
  github_usernames?: Array<string> | undefined;
};
/** @internal */
export declare const PublishPluginsRequestBody$outboundSchema: z.ZodMiniType<
  PublishPluginsRequestBody$Outbound,
  PublishPluginsRequestBody
>;
export declare function publishPluginsRequestBodyToJSON(
  publishPluginsRequestBody: PublishPluginsRequestBody,
): string;
//# sourceMappingURL=publishpluginsrequestbody.d.ts.map
