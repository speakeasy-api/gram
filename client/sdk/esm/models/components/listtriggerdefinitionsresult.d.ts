import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TriggerDefinition } from "./triggerdefinition.js";
export type ListTriggerDefinitionsResult = {
  /**
   * The available trigger definitions.
   */
  definitions: Array<TriggerDefinition>;
};
/** @internal */
export declare const ListTriggerDefinitionsResult$inboundSchema: z.ZodMiniType<
  ListTriggerDefinitionsResult,
  unknown
>;
export declare function listTriggerDefinitionsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListTriggerDefinitionsResult, SDKValidationError>;
//# sourceMappingURL=listtriggerdefinitionsresult.d.ts.map
