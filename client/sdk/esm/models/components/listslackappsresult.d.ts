import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { SlackAppResult } from "./slackappresult.js";
export type ListSlackAppsResult = {
  /**
   * List of Slack apps
   */
  items: Array<SlackAppResult>;
};
/** @internal */
export declare const ListSlackAppsResult$inboundSchema: z.ZodMiniType<
  ListSlackAppsResult,
  unknown
>;
export declare function listSlackAppsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListSlackAppsResult, SDKValidationError>;
//# sourceMappingURL=listslackappsresult.d.ts.map
