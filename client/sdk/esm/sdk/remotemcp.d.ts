import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListServersResult } from "../models/components/listserversresult.js";
import { ProtectedResourceMetadataDiscovery } from "../models/components/protectedresourcemetadatadiscovery.js";
import { RemoteMcpServer } from "../models/components/remotemcpserver.js";
import { VerifyURLResult } from "../models/components/verifyurlresult.js";
import {
  CreateRemoteMcpServerRequest,
  CreateRemoteMcpServerSecurity,
} from "../models/operations/createremotemcpserver.js";
import {
  DeleteRemoteMcpServerRequest,
  DeleteRemoteMcpServerSecurity,
} from "../models/operations/deleteremotemcpserver.js";
import {
  DiscoverRemoteMcpProtectedResourceMetadataRequest,
  DiscoverRemoteMcpProtectedResourceMetadataSecurity,
} from "../models/operations/discoverremotemcpprotectedresourcemetadata.js";
import {
  GetRemoteMcpServerRequest,
  GetRemoteMcpServerSecurity,
} from "../models/operations/getremotemcpserver.js";
import {
  ListRemoteMcpServersRequest,
  ListRemoteMcpServersSecurity,
} from "../models/operations/listremotemcpservers.js";
import {
  UpdateRemoteMcpServerRequest,
  UpdateRemoteMcpServerSecurity,
} from "../models/operations/updateremotemcpserver.js";
import {
  VerifyRemoteMcpURLRequest,
  VerifyRemoteMcpURLSecurity,
} from "../models/operations/verifyremotemcpurl.js";
export declare class RemoteMcp extends ClientSDK {
  /**
   * createServer remoteMcp
   *
   * @remarks
   * Create a new remote MCP server
   */
  createServer(
    request: CreateRemoteMcpServerRequest,
    security?: CreateRemoteMcpServerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteMcpServer>;
  /**
   * deleteServer remoteMcp
   *
   * @remarks
   * Delete a remote MCP server
   */
  deleteServer(
    request: DeleteRemoteMcpServerRequest,
    security?: DeleteRemoteMcpServerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * discoverProtectedResourceMetadata remoteMcp
   *
   * @remarks
   * Probe the remote MCP server's origin for an RFC 9728 .well-known/oauth-protected-resource document and return either the parsed metadata or a typed unavailability reason. Runs server-side under guardian.Policy so production resource servers without CORS can still be inspected.
   */
  discoverProtectedResourceMetadata(
    request: DiscoverRemoteMcpProtectedResourceMetadataRequest,
    security?: DiscoverRemoteMcpProtectedResourceMetadataSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ProtectedResourceMetadataDiscovery>;
  /**
   * getServer remoteMcp
   *
   * @remarks
   * Get a remote MCP server by ID or slug. Exactly one of id or slug must be provided.
   */
  getServer(
    request?: GetRemoteMcpServerRequest | undefined,
    security?: GetRemoteMcpServerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteMcpServer>;
  /**
   * listServers remoteMcp
   *
   * @remarks
   * List all remote MCP servers for a project
   */
  listServers(
    request?: ListRemoteMcpServersRequest | undefined,
    security?: ListRemoteMcpServersSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListServersResult>;
  /**
   * updateServer remoteMcp
   *
   * @remarks
   * Update a remote MCP server
   */
  updateServer(
    request: UpdateRemoteMcpServerRequest,
    security?: UpdateRemoteMcpServerSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RemoteMcpServer>;
  /**
   * verifyURL remoteMcp
   *
   * @remarks
   * Probe a candidate remote MCP server URL by issuing an MCP initialize request and reporting the outcome. Used to give users a reachability signal before they save a new or updated remote MCP server. Treats reachable-but-401/403 responses as verified — auth verification is intentionally out of scope.
   */
  verifyURL(
    request: VerifyRemoteMcpURLRequest,
    security?: VerifyRemoteMcpURLSecurity | undefined,
    options?: RequestOptions,
  ): Promise<VerifyURLResult>;
}
//# sourceMappingURL=remotemcp.d.ts.map
