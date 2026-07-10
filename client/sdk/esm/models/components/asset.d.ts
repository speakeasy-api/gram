import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const Kind: {
    readonly Openapiv3: "openapiv3";
    readonly Image: "image";
    readonly Functions: "functions";
    readonly ChatAttachment: "chat_attachment";
    readonly Unknown: "unknown";
};
export type Kind = ClosedEnum<typeof Kind>;
export type Asset = {
    /**
     * The content length of the asset
     */
    contentLength: number;
    /**
     * The content type of the asset
     */
    contentType: string;
    /**
     * The creation date of the asset.
     */
    createdAt: Date;
    /**
     * The ID of the asset
     */
    id: string;
    kind: Kind;
    /**
     * The SHA256 hash of the asset
     */
    sha256: string;
    /**
     * The last update date of the asset.
     */
    updatedAt: Date;
};
/** @internal */
export declare const Kind$inboundSchema: z.ZodMiniEnum<typeof Kind>;
/** @internal */
export declare const Asset$inboundSchema: z.ZodMiniType<Asset, unknown>;
export declare function assetFromJSON(jsonString: string): SafeParseResult<Asset, SDKValidationError>;
//# sourceMappingURL=asset.d.ts.map