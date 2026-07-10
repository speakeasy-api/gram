import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { FunctionResourceDefinition } from "./functionresourcedefinition.js";
/**
 * A polymorphic resource - currently only function resources are supported
 */
export type Resource = {
    /**
     * A function resource
     */
    functionResourceDefinition?: FunctionResourceDefinition | undefined;
};
/** @internal */
export declare const Resource$inboundSchema: z.ZodMiniType<Resource, unknown>;
export declare function resourceFromJSON(jsonString: string): SafeParseResult<Resource, SDKValidationError>;
//# sourceMappingURL=resource.d.ts.map