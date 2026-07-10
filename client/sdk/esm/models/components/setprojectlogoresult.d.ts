import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Project } from "./project.js";
export type SetProjectLogoResult = {
    project: Project;
};
/** @internal */
export declare const SetProjectLogoResult$inboundSchema: z.ZodMiniType<SetProjectLogoResult, unknown>;
export declare function setProjectLogoResultFromJSON(jsonString: string): SafeParseResult<SetProjectLogoResult, SDKValidationError>;
//# sourceMappingURL=setprojectlogoresult.d.ts.map