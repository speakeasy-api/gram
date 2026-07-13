import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { IntegrationEntry } from "./integrationentry.js";
export type ListIntegrationsResult = {
  /**
   * List of available third-party integrations
   */
  integrations?: Array<IntegrationEntry> | undefined;
};
/** @internal */
export declare const ListIntegrationsResult$inboundSchema: z.ZodMiniType<
  ListIntegrationsResult,
  unknown
>;
export declare function listIntegrationsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListIntegrationsResult, SDKValidationError>;
//# sourceMappingURL=listintegrationsresult.d.ts.map
