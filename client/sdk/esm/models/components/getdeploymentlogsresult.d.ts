import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { DeploymentLogEvent } from "./deploymentlogevent.js";
export type GetDeploymentLogsResult = {
  /**
   * The logs for the deployment
   */
  events: Array<DeploymentLogEvent>;
  /**
   * The cursor to fetch results from
   */
  nextCursor?: string | undefined;
  /**
   * The status of the deployment
   */
  status: string;
};
/** @internal */
export declare const GetDeploymentLogsResult$inboundSchema: z.ZodMiniType<
  GetDeploymentLogsResult,
  unknown
>;
export declare function getDeploymentLogsResultFromJSON(
  jsonString: string,
): SafeParseResult<GetDeploymentLogsResult, SDKValidationError>;
//# sourceMappingURL=getdeploymentlogsresult.d.ts.map
