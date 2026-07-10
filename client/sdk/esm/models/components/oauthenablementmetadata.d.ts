import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type OAuthEnablementMetadata = {
  /**
   * Count of security variables that are OAuth2 supported
   */
  oauth2SecurityCount: number;
};
/** @internal */
export declare const OAuthEnablementMetadata$inboundSchema: z.ZodMiniType<
  OAuthEnablementMetadata,
  unknown
>;
export declare function oAuthEnablementMetadataFromJSON(
  jsonString: string,
): SafeParseResult<OAuthEnablementMetadata, SDKValidationError>;
//# sourceMappingURL=oauthenablementmetadata.d.ts.map
