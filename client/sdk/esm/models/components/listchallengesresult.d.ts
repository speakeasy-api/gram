import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AuthzChallenge } from "./authzchallenge.js";
export type ListChallengesResult = {
  /**
   * The challenge events.
   */
  challenges: Array<AuthzChallenge>;
  /**
   * Total number of matching challenges for pagination.
   */
  total: number;
};
/** @internal */
export declare const ListChallengesResult$inboundSchema: z.ZodMiniType<
  ListChallengesResult,
  unknown
>;
export declare function listChallengesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListChallengesResult, SDKValidationError>;
//# sourceMappingURL=listchallengesresult.d.ts.map
