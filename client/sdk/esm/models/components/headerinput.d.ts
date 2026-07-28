import * as z from "zod/v4-mini";
/**
 * Input for a remote MCP server header
 */
export type HeaderInput = {
  /**
   * Description of the header
   */
  description?: string | undefined;
  /**
   * Whether the header is required
   */
  isRequired?: boolean | undefined;
  /**
   * Whether the header value is a secret
   */
  isSecret?: boolean | undefined;
  /**
   * The header name
   */
  name: string;
  /**
   * Static header value (mutually exclusive with value_from_request_header)
   */
  value?: string | undefined;
  /**
   * Name of the inbound request header to pass through (mutually exclusive with value)
   */
  valueFromRequestHeader?: string | undefined;
};
/** @internal */
export type HeaderInput$Outbound = {
  description?: string | undefined;
  is_required?: boolean | undefined;
  is_secret?: boolean | undefined;
  name: string;
  value?: string | undefined;
  value_from_request_header?: string | undefined;
};
/** @internal */
export declare const HeaderInput$outboundSchema: z.ZodMiniType<
  HeaderInput$Outbound,
  HeaderInput
>;
export declare function headerInputToJSON(headerInput: HeaderInput): string;
//# sourceMappingURL=headerinput.d.ts.map
