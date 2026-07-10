import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { IntegrationVersion } from "./integrationversion.js";
export type Integration = {
    packageDescription?: string | undefined;
    packageDescriptionRaw?: string | undefined;
    packageId: string;
    packageImageAssetId?: string | undefined;
    packageKeywords?: Array<string> | undefined;
    packageName: string;
    packageSummary: string;
    packageTitle: string;
    packageUrl?: string | undefined;
    toolNames: Array<string>;
    /**
     * The latest version of the integration
     */
    version: string;
    versionCreatedAt: Date;
    versions?: Array<IntegrationVersion> | undefined;
};
/** @internal */
export declare const Integration$inboundSchema: z.ZodMiniType<Integration, unknown>;
export declare function integrationFromJSON(jsonString: string): SafeParseResult<Integration, SDKValidationError>;
//# sourceMappingURL=integration.d.ts.map