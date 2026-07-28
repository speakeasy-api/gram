import * as z from "zod/v4-mini";
export type WorkOSSSOIntentOptions = {
  /**
   * SSO bookmark slug to launch a specific app after authentication.
   */
  bookmarkSlug?: string | undefined;
  /**
   * SSO provider type to shortcut into a specific setup flow (e.g. OktaSAML, GoogleSAML).
   */
  providerType?: string | undefined;
};
/** @internal */
export type WorkOSSSOIntentOptions$Outbound = {
  bookmark_slug?: string | undefined;
  provider_type?: string | undefined;
};
/** @internal */
export declare const WorkOSSSOIntentOptions$outboundSchema: z.ZodMiniType<
  WorkOSSSOIntentOptions$Outbound,
  WorkOSSSOIntentOptions
>;
export declare function workOSSSOIntentOptionsToJSON(
  workOSSSOIntentOptions: WorkOSSSOIntentOptions,
): string;
//# sourceMappingURL=workosssointentoptions.d.ts.map
