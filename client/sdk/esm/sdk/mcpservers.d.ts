import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListMcpServersResult } from "../models/components/listmcpserversresult.js";
import { ListToolFiltersResult } from "../models/components/listtoolfiltersresult.js";
import { McpServer } from "../models/components/mcpserver.js";
import {
  CreateMcpServerRequest,
  CreateMcpServerSecurity,
} from "../models/operations/createmcpserver.js";
import {
  DeleteMcpServerRequest,
  DeleteMcpServerSecurity,
} from "../models/operations/deletemcpserver.js";
import {
  GetMcpServerRequest,
  GetMcpServerSecurity,
} from "../models/operations/getmcpserver.js";
import {
  ListMcpServersRequest,
  ListMcpServersSecurity,
} from "../models/operations/listmcpservers.js";
import {
  ListMcpServerToolFiltersRequest,
  ListMcpServerToolFiltersSecurity,
} from "../models/operations/listmcpservertoolfilters.js";
import {
  UpdateMcpServerRequest,
  UpdateMcpServerSecurity,
} from "../models/operations/updatemcpserver.js";
export declare class McpServers extends ClientSDK {
  /**
   * createMcpServer mcpServers
   *
   * @remarks
   * Create a new MCP server
   */
  create(
    request: CreateMcpServerRequest,
    security?: CreateMcpServerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<McpServer>;
  /**
   * deleteMcpServer mcpServers
   *
   * @remarks
   * Delete an MCP server
   */
  delete(
    request: DeleteMcpServerRequest,
    security?: DeleteMcpServerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getMcpServer mcpServers
   *
   * @remarks
   * Get an MCP server by ID or slug. Exactly one of id or slug must be provided.
   */
  get(
    request?: GetMcpServerRequest | undefined,
    security?: GetMcpServerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<McpServer>;
  /**
   * listMcpServers mcpServers
   *
   * @remarks
   * List MCP servers for a project. Accepts optional remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id filters to scope the result to a single backend; at most one filter may be supplied since the backends are mutually exclusive.
   */
  list(
    request?: ListMcpServersRequest | undefined,
    security?: ListMcpServersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListMcpServersResult>;
  /**
   * listToolFilters mcpServers
   *
   * @remarks
   * List the tool filter scopes (tags) available on an MCP server and the tools under each, including tools excluded from all filters. Exactly one of id or slug must be provided. Read-only; reflects the explicit tool variations group resolved from the chain (mcp_servers then toolsets), deriving effective tags with the same logic as the runtime ?tags= filter. Returns filtering disabled when no explicit group is set.
   */
  listToolFilters(
    request?: ListMcpServerToolFiltersRequest | undefined,
    security?: ListMcpServerToolFiltersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListToolFiltersResult>;
  /**
   * updateMcpServer mcpServers
   *
   * @remarks
   * Update an MCP server. This is a full-record replace for the optional UUID references: fields omitted from the request become null on the stored record. name is an exception — omitting it leaves the existing display name unchanged, while providing it requires a non-empty value and recomputes the server-side slug. The id and visibility fields are required; exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be provided.
   */
  update(
    request: UpdateMcpServerRequest,
    security?: UpdateMcpServerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<McpServer>;
}
//# sourceMappingURL=mcpservers.d.ts.map
