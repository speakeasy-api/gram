import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolVariationGroup } from "./toolvariationgroup.js";
export type ListToolVariationGroupsResult = {
    groups: Array<ToolVariationGroup>;
};
/** @internal */
export declare const ListToolVariationGroupsResult$inboundSchema: z.ZodMiniType<ListToolVariationGroupsResult, unknown>;
export declare function listToolVariationGroupsResultFromJSON(jsonString: string): SafeParseResult<ListToolVariationGroupsResult, SDKValidationError>;
//# sourceMappingURL=listtoolvariationgroupsresult.d.ts.map