import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A header configured for a remote MCP server
 */
export type RemoteMcpServerHeader = {
  /**
   * When the header was created
   */
  createdAt: Date;
  /**
   * Description of the header
   */
  description?: string | undefined;
  /**
   * The ID of the header
   */
  id: string;
  /**
   * Whether the header is required
   */
  isRequired: boolean;
  /**
   * Whether the header value is a secret
   */
  isSecret: boolean;
  /**
   * The header name
   */
  name: string;
  /**
   * When the header was last updated
   */
  updatedAt: Date;
  /**
   * The header value (redacted if secret)
   */
  value?: string | undefined;
  /**
   * Name of the inbound request header to pass through
   */
  valueFromRequestHeader?: string | undefined;
};
/** @internal */
export declare const RemoteMcpServerHeader$inboundSchema: z.ZodMiniType<
  RemoteMcpServerHeader,
  unknown
>;
export declare function remoteMcpServerHeaderFromJSON(
  jsonString: string,
): SafeParseResult<RemoteMcpServerHeader, SDKValidationError>;
//# sourceMappingURL=remotemcpserverheader.d.ts.map
