import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Project } from "./project.js";
export type CreateProjectResult = {
  project: Project;
};
/** @internal */
export declare const CreateProjectResult$inboundSchema: z.ZodMiniType<
  CreateProjectResult,
  unknown
>;
export declare function createProjectResultFromJSON(
  jsonString: string,
): SafeParseResult<CreateProjectResult, SDKValidationError>;
//# sourceMappingURL=createprojectresult.d.ts.map
