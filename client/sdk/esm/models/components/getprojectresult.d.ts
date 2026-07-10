import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Project } from "./project.js";
export type GetProjectResult = {
  project: Project;
};
/** @internal */
export declare const GetProjectResult$inboundSchema: z.ZodMiniType<
  GetProjectResult,
  unknown
>;
export declare function getProjectResultFromJSON(
  jsonString: string,
): SafeParseResult<GetProjectResult, SDKValidationError>;
//# sourceMappingURL=getprojectresult.d.ts.map
