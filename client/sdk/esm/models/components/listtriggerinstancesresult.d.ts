import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TriggerInstance } from "./triggerinstance.js";
export type ListTriggerInstancesResult = {
  /**
   * The trigger instances for the current project.
   */
  triggers: Array<TriggerInstance>;
};
/** @internal */
export declare const ListTriggerInstancesResult$inboundSchema: z.ZodMiniType<
  ListTriggerInstancesResult,
  unknown
>;
export declare function listTriggerInstancesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListTriggerInstancesResult, SDKValidationError>;
//# sourceMappingURL=listtriggerinstancesresult.d.ts.map
