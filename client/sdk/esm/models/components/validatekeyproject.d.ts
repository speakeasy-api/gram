import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ValidateKeyProject = {
    /**
     * The ID of the project
     */
    id: string;
    /**
     * The name of the project
     */
    name: string;
    /**
     * The slug of the project
     */
    slug: string;
};
/** @internal */
export declare const ValidateKeyProject$inboundSchema: z.ZodMiniType<ValidateKeyProject, unknown>;
export declare function validateKeyProjectFromJSON(jsonString: string): SafeParseResult<ValidateKeyProject, SDKValidationError>;
//# sourceMappingURL=validatekeyproject.d.ts.map