import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Result of listing distinct attribute keys
 */
export type ListAttributeKeysResult = {
    /**
     * Distinct attribute keys. User attributes are prefixed with @
     */
    keys: Array<string>;
};
/** @internal */
export declare const ListAttributeKeysResult$inboundSchema: z.ZodMiniType<ListAttributeKeysResult, unknown>;
export declare function listAttributeKeysResultFromJSON(jsonString: string): SafeParseResult<ListAttributeKeysResult, SDKValidationError>;
//# sourceMappingURL=listattributekeysresult.d.ts.map