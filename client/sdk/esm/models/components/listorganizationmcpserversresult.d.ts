import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OrganizationMcpServer } from "./organizationmcpserver.js";
/**
 * Result type for the MCP servers attached to a remote_session_client.
 */
export type ListOrganizationMcpServersResult = {
    items: Array<OrganizationMcpServer>;
};
/** @internal */
export declare const ListOrganizationMcpServersResult$inboundSchema: z.ZodMiniType<ListOrganizationMcpServersResult, unknown>;
export declare function listOrganizationMcpServersResultFromJSON(jsonString: string): SafeParseResult<ListOrganizationMcpServersResult, SDKValidationError>;
//# sourceMappingURL=listorganizationmcpserversresult.d.ts.map