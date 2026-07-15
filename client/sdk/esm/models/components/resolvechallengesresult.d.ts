import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ChallengeResolution } from "./challengeresolution.js";
export type ResolveChallengesResult = {
  /**
   * The created resolution records.
   */
  resolutions: Array<ChallengeResolution>;
};
/** @internal */
export declare const ResolveChallengesResult$inboundSchema: z.ZodMiniType<
  ResolveChallengesResult,
  unknown
>;
export declare function resolveChallengesResultFromJSON(
  jsonString: string,
): SafeParseResult<ResolveChallengesResult, SDKValidationError>;
//# sourceMappingURL=resolvechallengesresult.d.ts.map
