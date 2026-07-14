import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { SlackAppResult } from "./slackappresult.js";
export type CreateSlackAppResult = {
  app: SlackAppResult;
};
/** @internal */
export declare const CreateSlackAppResult$inboundSchema: z.ZodMiniType<
  CreateSlackAppResult,
  unknown
>;
export declare function createSlackAppResultFromJSON(
  jsonString: string,
): SafeParseResult<CreateSlackAppResult, SDKValidationError>;
//# sourceMappingURL=createslackappresult.d.ts.map
