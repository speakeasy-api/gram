import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TunneledMcpServer } from "./tunneledmcpserver.js";
/**
 * Created tunneled MCP server plus the one-time tunnel key
 */
export type CreateTunneledMcpServerResult = {
    /**
     * A customer-hosted MCP server connected through a tunnel
     */
    server: TunneledMcpServer;
    /**
     * Plaintext tunnel key. Only returned at creation time.
     */
    tunnelKey: string;
};
/** @internal */
export declare const CreateTunneledMcpServerResult$inboundSchema: z.ZodMiniType<CreateTunneledMcpServerResult, unknown>;
export declare function createTunneledMcpServerResultFromJSON(jsonString: string): SafeParseResult<CreateTunneledMcpServerResult, SDKValidationError>;
//# sourceMappingURL=createtunneledmcpserverresult.d.ts.map