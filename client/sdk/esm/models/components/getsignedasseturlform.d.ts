import * as z from "zod/v4-mini";
export type GetSignedAssetURLForm = {
    /**
     * The ID of the function asset
     */
    assetId: string;
};
/** @internal */
export type GetSignedAssetURLForm$Outbound = {
    asset_id: string;
};
/** @internal */
export declare const GetSignedAssetURLForm$outboundSchema: z.ZodMiniType<GetSignedAssetURLForm$Outbound, GetSignedAssetURLForm>;
export declare function getSignedAssetURLFormToJSON(getSignedAssetURLForm: GetSignedAssetURLForm): string;
//# sourceMappingURL=getsignedasseturlform.d.ts.map