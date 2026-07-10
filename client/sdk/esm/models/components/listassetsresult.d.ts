import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Asset } from "./asset.js";
export type ListAssetsResult = {
    /**
     * The list of assets
     */
    assets: Array<Asset>;
};
/** @internal */
export declare const ListAssetsResult$inboundSchema: z.ZodMiniType<ListAssetsResult, unknown>;
export declare function listAssetsResultFromJSON(jsonString: string): SafeParseResult<ListAssetsResult, SDKValidationError>;
//# sourceMappingURL=listassetsresult.d.ts.map