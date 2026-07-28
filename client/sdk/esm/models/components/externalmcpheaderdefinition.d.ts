import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ExternalMCPHeaderDefinition = {
  /**
   * Description of the header
   */
  description?: string | undefined;
  /**
   * The actual HTTP header name to send (e.g., X-Api-Key)
   */
  headerName: string;
  /**
   * The prefixed environment variable name (e.g., SLACK_X_API_KEY)
   */
  name: string;
  /**
   * Placeholder value for the header
   */
  placeholder?: string | undefined;
  /**
   * Whether the header is required
   */
  required: boolean;
  /**
   * Whether the header value is secret
   */
  secret: boolean;
};
/** @internal */
export declare const ExternalMCPHeaderDefinition$inboundSchema: z.ZodMiniType<
  ExternalMCPHeaderDefinition,
  unknown
>;
export declare function externalMCPHeaderDefinitionFromJSON(
  jsonString: string,
): SafeParseResult<ExternalMCPHeaderDefinition, SDKValidationError>;
//# sourceMappingURL=externalmcpheaderdefinition.d.ts.map
