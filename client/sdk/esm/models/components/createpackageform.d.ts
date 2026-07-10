import * as z from "zod/v4-mini";
export type CreatePackageForm = {
    /**
     * The description of the package. Limited markdown syntax is supported.
     */
    description?: string | undefined;
    /**
     * The asset ID of the image to show for this package
     */
    imageAssetId?: string | undefined;
    /**
     * The keywords of the package
     */
    keywords?: Array<string> | undefined;
    /**
     * The name of the package
     */
    name: string;
    /**
     * The summary of the package
     */
    summary: string;
    /**
     * The title of the package
     */
    title: string;
    /**
     * External URL for the package owner
     */
    url?: string | undefined;
};
/** @internal */
export type CreatePackageForm$Outbound = {
    description?: string | undefined;
    image_asset_id?: string | undefined;
    keywords?: Array<string> | undefined;
    name: string;
    summary: string;
    title: string;
    url?: string | undefined;
};
/** @internal */
export declare const CreatePackageForm$outboundSchema: z.ZodMiniType<CreatePackageForm$Outbound, CreatePackageForm>;
export declare function createPackageFormToJSON(createPackageForm: CreatePackageForm): string;
//# sourceMappingURL=createpackageform.d.ts.map