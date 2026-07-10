import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type PackageVersion = {
    /**
     * The creation date of the package version
     */
    createdAt: Date;
    /**
     * The ID of the deployment that the version belongs to
     */
    deploymentId: string;
    /**
     * The ID of the package version
     */
    id: string;
    /**
     * The ID of the package that the version belongs to
     */
    packageId: string;
    /**
     * The semantic version value
     */
    semver: string;
    /**
     * The visibility of the package version
     */
    visibility: string;
};
/** @internal */
export declare const PackageVersion$inboundSchema: z.ZodMiniType<PackageVersion, unknown>;
export declare function packageVersionFromJSON(jsonString: string): SafeParseResult<PackageVersion, SDKValidationError>;
//# sourceMappingURL=packageversion.d.ts.map