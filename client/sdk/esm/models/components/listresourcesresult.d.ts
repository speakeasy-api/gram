import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Resource } from "./resource.js";
export type ListResourcesResult = {
    /**
     * The cursor to fetch results from
     */
    nextCursor?: string | undefined;
    /**
     * The list of resources
     */
    resources: Array<Resource>;
};
/** @internal */
export declare const ListResourcesResult$inboundSchema: z.ZodMiniType<ListResourcesResult, unknown>;
export declare function listResourcesResultFromJSON(jsonString: string): SafeParseResult<ListResourcesResult, SDKValidationError>;
//# sourceMappingURL=listresourcesresult.d.ts.map