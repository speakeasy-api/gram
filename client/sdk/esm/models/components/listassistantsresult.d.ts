import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Assistant } from "./assistant.js";
export type ListAssistantsResult = {
    /**
     * Assistants for the current project.
     */
    assistants: Array<Assistant>;
};
/** @internal */
export declare const ListAssistantsResult$inboundSchema: z.ZodMiniType<ListAssistantsResult, unknown>;
export declare function listAssistantsResultFromJSON(jsonString: string): SafeParseResult<ListAssistantsResult, SDKValidationError>;
//# sourceMappingURL=listassistantsresult.d.ts.map