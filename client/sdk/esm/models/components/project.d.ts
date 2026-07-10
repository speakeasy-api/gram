import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type Project = {
    /**
     * The creation date of the project.
     */
    createdAt: Date;
    /**
     * The ID of the project
     */
    id: string;
    /**
     * The ID of the logo asset for the project
     */
    logoAssetId?: string | undefined;
    /**
     * The name of the project
     */
    name: string;
    /**
     * The ID of the organization that owns the project
     */
    organizationId: string;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    slug: string;
    /**
     * The last update date of the project.
     */
    updatedAt: Date;
};
/** @internal */
export declare const Project$inboundSchema: z.ZodMiniType<Project, unknown>;
export declare function projectFromJSON(jsonString: string): SafeParseResult<Project, SDKValidationError>;
//# sourceMappingURL=project.d.ts.map