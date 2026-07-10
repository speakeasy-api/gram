import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ProjectEntry } from "./projectentry.js";
export type ListProjectsResult = {
    /**
     * The list of projects
     */
    projects: Array<ProjectEntry>;
};
/** @internal */
export declare const ListProjectsResult$inboundSchema: z.ZodMiniType<ListProjectsResult, unknown>;
export declare function listProjectsResultFromJSON(jsonString: string): SafeParseResult<ListProjectsResult, SDKValidationError>;
//# sourceMappingURL=listprojectsresult.d.ts.map