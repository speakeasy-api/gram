import * as z from "zod/v4-mini";
import { EnvironmentEntryInput, EnvironmentEntryInput$Outbound } from "./environmententryinput.js";
export type UpdateEnvironmentRequestBody = {
    /**
     * The description of the environment
     */
    description?: string | undefined;
    /**
     * List of environment entry names to remove
     */
    entriesToRemove: Array<string>;
    /**
     * List of environment entries to update or create
     */
    entriesToUpdate: Array<EnvironmentEntryInput>;
    /**
     * The name of the environment
     */
    name?: string | undefined;
};
/** @internal */
export type UpdateEnvironmentRequestBody$Outbound = {
    description?: string | undefined;
    entries_to_remove: Array<string>;
    entries_to_update: Array<EnvironmentEntryInput$Outbound>;
    name?: string | undefined;
};
/** @internal */
export declare const UpdateEnvironmentRequestBody$outboundSchema: z.ZodMiniType<UpdateEnvironmentRequestBody$Outbound, UpdateEnvironmentRequestBody>;
export declare function updateEnvironmentRequestBodyToJSON(updateEnvironmentRequestBody: UpdateEnvironmentRequestBody): string;
//# sourceMappingURL=updateenvironmentrequestbody.d.ts.map