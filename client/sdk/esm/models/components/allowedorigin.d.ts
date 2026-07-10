import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export declare const AllowedOriginStatus: {
    readonly Pending: "pending";
    readonly Approved: "approved";
    readonly Rejected: "rejected";
};
export type AllowedOriginStatus = ClosedEnum<typeof AllowedOriginStatus>;
export type AllowedOrigin = {
    /**
     * The creation date of the allowed origin.
     */
    createdAt: Date;
    /**
     * The ID of the allowed origin
     */
    id: string;
    /**
     * The origin URL
     */
    origin: string;
    /**
     * The ID of the project
     */
    projectId: string;
    status: AllowedOriginStatus;
    /**
     * The last update date of the allowed origin.
     */
    updatedAt: Date;
};
/** @internal */
export declare const AllowedOriginStatus$inboundSchema: z.ZodMiniEnum<typeof AllowedOriginStatus>;
/** @internal */
export declare const AllowedOrigin$inboundSchema: z.ZodMiniType<AllowedOrigin, unknown>;
export declare function allowedOriginFromJSON(jsonString: string): SafeParseResult<AllowedOrigin, SDKValidationError>;
//# sourceMappingURL=allowedorigin.d.ts.map