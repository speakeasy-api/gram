import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DeploymentLogEvent = {
  /**
   * The ID of the asset tied to the log event
   */
  attachmentId?: string | undefined;
  /**
   * The type of the asset tied to the log event
   */
  attachmentType?: string | undefined;
  /**
   * The creation date of the log event
   */
  createdAt: string;
  /**
   * The type of event that occurred
   */
  event: string;
  /**
   * The ID of the log event
   */
  id: string;
  /**
   * The message of the log event
   */
  message: string;
};
/** @internal */
export declare const DeploymentLogEvent$inboundSchema: z.ZodMiniType<
  DeploymentLogEvent,
  unknown
>;
export declare function deploymentLogEventFromJSON(
  jsonString: string,
): SafeParseResult<DeploymentLogEvent, SDKValidationError>;
//# sourceMappingURL=deploymentlogevent.d.ts.map
