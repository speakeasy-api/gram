import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type IntegrationVersion = {
    createdAt: Date;
    version: string;
};
/** @internal */
export declare const IntegrationVersion$inboundSchema: z.ZodMiniType<IntegrationVersion, unknown>;
export declare function integrationVersionFromJSON(jsonString: string): SafeParseResult<IntegrationVersion, SDKValidationError>;
//# sourceMappingURL=integrationversion.d.ts.map