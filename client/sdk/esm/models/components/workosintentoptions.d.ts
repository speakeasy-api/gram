import * as z from "zod/v4-mini";
import {
  WorkOSDomainVerificationIntentOptions,
  WorkOSDomainVerificationIntentOptions$Outbound,
} from "./workosdomainverificationintentoptions.js";
import {
  WorkOSSSOIntentOptions,
  WorkOSSSOIntentOptions$Outbound,
} from "./workosssointentoptions.js";
export type WorkOSIntentOptions = {
  domainVerification?: WorkOSDomainVerificationIntentOptions | undefined;
  sso?: WorkOSSSOIntentOptions | undefined;
};
/** @internal */
export type WorkOSIntentOptions$Outbound = {
  domain_verification?:
    | WorkOSDomainVerificationIntentOptions$Outbound
    | undefined;
  sso?: WorkOSSSOIntentOptions$Outbound | undefined;
};
/** @internal */
export declare const WorkOSIntentOptions$outboundSchema: z.ZodMiniType<
  WorkOSIntentOptions$Outbound,
  WorkOSIntentOptions
>;
export declare function workOSIntentOptionsToJSON(
  workOSIntentOptions: WorkOSIntentOptions,
): string;
//# sourceMappingURL=workosintentoptions.d.ts.map
