import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type OnboardingStatusResult = {
  /**
   * Whether the organization has at least one linked directory sync in WorkOS.
   */
  dsyncConfigured: boolean;
  /**
   * Whether the organization has at least one active SSO connection in WorkOS.
   */
  ssoConfigured: boolean;
};
/** @internal */
export declare const OnboardingStatusResult$inboundSchema: z.ZodMiniType<
  OnboardingStatusResult,
  unknown
>;
export declare function onboardingStatusResultFromJSON(
  jsonString: string,
): SafeParseResult<OnboardingStatusResult, SDKValidationError>;
//# sourceMappingURL=onboardingstatusresult.d.ts.map
