import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type OpenAPIv3DeploymentAsset = {
    /**
     * The ID of the uploaded asset.
     */
    assetId: string;
    /**
     * The ID of the deployment asset.
     */
    id: string;
    /**
     * The name to give the document as it will be displayed in UIs.
     */
    name: string;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    slug: string;
};
/** @internal */
export declare const OpenAPIv3DeploymentAsset$inboundSchema: z.ZodMiniType<OpenAPIv3DeploymentAsset, unknown>;
export declare function openAPIv3DeploymentAssetFromJSON(jsonString: string): SafeParseResult<OpenAPIv3DeploymentAsset, SDKValidationError>;
//# sourceMappingURL=openapiv3deploymentasset.d.ts.map