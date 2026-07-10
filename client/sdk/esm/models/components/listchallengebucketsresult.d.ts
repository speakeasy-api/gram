import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ChallengeBucket } from "./challengebucket.js";
export type ListChallengeBucketsResult = {
  /**
   * The challenge buckets.
   */
  buckets: Array<ChallengeBucket>;
  /**
   * Total number of matching buckets for pagination.
   */
  total: number;
};
/** @internal */
export declare const ListChallengeBucketsResult$inboundSchema: z.ZodMiniType<
  ListChallengeBucketsResult,
  unknown
>;
export declare function listChallengeBucketsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListChallengeBucketsResult, SDKValidationError>;
//# sourceMappingURL=listchallengebucketsresult.d.ts.map
