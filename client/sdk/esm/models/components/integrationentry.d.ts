import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type IntegrationEntry = {
  packageId: string;
  packageImageAssetId?: string | undefined;
  packageKeywords?: Array<string> | undefined;
  packageName: string;
  packageSummary?: string | undefined;
  packageTitle?: string | undefined;
  packageUrl?: string | undefined;
  toolNames: Array<string>;
  version: string;
  versionCreatedAt: Date;
};
/** @internal */
export declare const IntegrationEntry$inboundSchema: z.ZodMiniType<
  IntegrationEntry,
  unknown
>;
export declare function integrationEntryFromJSON(
  jsonString: string,
): SafeParseResult<IntegrationEntry, SDKValidationError>;
//# sourceMappingURL=integrationentry.d.ts.map
