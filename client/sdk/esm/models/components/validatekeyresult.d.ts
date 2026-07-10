import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ValidateKeyOrganization } from "./validatekeyorganization.js";
import { ValidateKeyProject } from "./validatekeyproject.js";
export type ValidateKeyResult = {
    organization: ValidateKeyOrganization;
    /**
     * The projects accessible with this key
     */
    projects: Array<ValidateKeyProject>;
    /**
     * List of permission scopes for this key
     */
    scopes: Array<string>;
};
/** @internal */
export declare const ValidateKeyResult$inboundSchema: z.ZodMiniType<ValidateKeyResult, unknown>;
export declare function validateKeyResultFromJSON(jsonString: string): SafeParseResult<ValidateKeyResult, SDKValidationError>;
//# sourceMappingURL=validatekeyresult.d.ts.map