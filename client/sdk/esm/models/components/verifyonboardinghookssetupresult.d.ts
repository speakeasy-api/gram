import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OnboardingHookEvent } from "./onboardinghookevent.js";
export type VerifyOnboardingHooksSetupResult = {
  /**
   * Recent hook events, newest first. Truncated to a server-defined limit.
   */
  events: Array<OnboardingHookEvent>;
  /**
   * Highest time_unix_nano in this batch. Pass back as since_unix_nano on the next poll.
   */
  latestUnixNano: string;
  /**
   * Total events received with time_unix_nano greater than since_unix_nano. May exceed len(events) when truncated.
   */
  totalCount: number;
};
/** @internal */
export declare const VerifyOnboardingHooksSetupResult$inboundSchema: z.ZodMiniType<
  VerifyOnboardingHooksSetupResult,
  unknown
>;
export declare function verifyOnboardingHooksSetupResultFromJSON(
  jsonString: string,
): SafeParseResult<VerifyOnboardingHooksSetupResult, SDKValidationError>;
//# sourceMappingURL=verifyonboardinghookssetupresult.d.ts.map
