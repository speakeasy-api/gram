import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Visibility of the collection
 */
export declare const UpdateRequestBodyVisibility: {
    readonly Public: "public";
    readonly Private: "private";
};
/**
 * Visibility of the collection
 */
export type UpdateRequestBodyVisibility = ClosedEnum<typeof UpdateRequestBodyVisibility>;
export type UpdateRequestBody = {
    /**
     * ID of the collection to update
     */
    collectionId: string;
    /**
     * Description of the collection
     */
    description?: string | undefined;
    /**
     * Display name for the collection
     */
    name?: string | undefined;
    /**
     * Visibility of the collection
     */
    visibility?: UpdateRequestBodyVisibility | undefined;
};
/** @internal */
export declare const UpdateRequestBodyVisibility$outboundSchema: z.ZodMiniEnum<typeof UpdateRequestBodyVisibility>;
/** @internal */
export type UpdateRequestBody$Outbound = {
    collection_id: string;
    description?: string | undefined;
    name?: string | undefined;
    visibility?: string | undefined;
};
/** @internal */
export declare const UpdateRequestBody$outboundSchema: z.ZodMiniType<UpdateRequestBody$Outbound, UpdateRequestBody>;
export declare function updateRequestBodyToJSON(updateRequestBody: UpdateRequestBody): string;
//# sourceMappingURL=updaterequestbody.d.ts.map