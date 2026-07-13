import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPRemoteHeader } from "./externalmcpremoteheader.js";
import { ExternalMCPRemoteVariable } from "./externalmcpremotevariable.js";
/**
 * Transport type (sse or streamable-http)
 */
export declare const TransportType: {
  readonly Sse: "sse";
  readonly StreamableHttp: "streamable-http";
};
/**
 * Transport type (sse or streamable-http)
 */
export type TransportType = ClosedEnum<typeof TransportType>;
/**
 * A remote endpoint for an MCP server
 */
export type ExternalMCPRemote = {
  /**
   * HTTP headers the MCP client should collect and send when connecting to this remote
   */
  headers?: Array<ExternalMCPRemoteHeader> | undefined;
  /**
   * Transport type (sse or streamable-http)
   */
  transportType: TransportType;
  /**
   * URL of the remote endpoint
   */
  url: string;
  /**
   * URL template variables for this remote endpoint
   */
  variables?:
    | {
        [k: string]: ExternalMCPRemoteVariable;
      }
    | undefined;
};
/** @internal */
export declare const TransportType$inboundSchema: z.ZodMiniEnum<
  typeof TransportType
>;
/** @internal */
export declare const ExternalMCPRemote$inboundSchema: z.ZodMiniType<
  ExternalMCPRemote,
  unknown
>;
export declare function externalMCPRemoteFromJSON(
  jsonString: string,
): SafeParseResult<ExternalMCPRemote, SDKValidationError>;
//# sourceMappingURL=externalmcpremote.d.ts.map
