import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type GetSlackConnectionResult = {
  /**
   * When the toolset was created.
   */
  createdAt: Date;
  /**
   * The default toolset slug for this Slack connection
   */
  defaultToolsetSlug: string;
  /**
   * The ID of the connected Slack team
   */
  slackTeamId: string;
  /**
   * The name of the connected Slack team
   */
  slackTeamName: string;
  /**
   * When the toolset was last updated.
   */
  updatedAt: Date;
};
/** @internal */
export declare const GetSlackConnectionResult$inboundSchema: z.ZodType<
  GetSlackConnectionResult,
  z.ZodTypeDef,
  unknown
>;
export declare function getSlackConnectionResultFromJSON(
  jsonString: string,
): SafeParseResult<GetSlackConnectionResult, SDKValidationError>;
//# sourceMappingURL=getslackconnectionresult.d.ts.map
