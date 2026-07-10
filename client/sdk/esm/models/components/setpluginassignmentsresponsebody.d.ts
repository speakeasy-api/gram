import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { PluginAssignment } from "./pluginassignment.js";
export type SetPluginAssignmentsResponseBody = {
    /**
     * The updated assignments.
     */
    assignments: Array<PluginAssignment>;
};
/** @internal */
export declare const SetPluginAssignmentsResponseBody$inboundSchema: z.ZodMiniType<SetPluginAssignmentsResponseBody, unknown>;
export declare function setPluginAssignmentsResponseBodyFromJSON(jsonString: string): SafeParseResult<SetPluginAssignmentsResponseBody, SDKValidationError>;
//# sourceMappingURL=setpluginassignmentsresponsebody.d.ts.map