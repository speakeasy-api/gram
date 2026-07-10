import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CreateTunneledMcpServerResult } from "../models/components/createtunneledmcpserverresult.js";
import { ListTunneledMcpServersResult } from "../models/components/listtunneledmcpserversresult.js";
import { RotateTunneledMcpServerKeyResult } from "../models/components/rotatetunneledmcpserverkeyresult.js";
import { TunneledMcpServer } from "../models/components/tunneledmcpserver.js";
import { TunneledMcpServerConnections } from "../models/components/tunneledmcpserverconnections.js";
import { CreateTunneledMcpServerRequest, CreateTunneledMcpServerSecurity } from "../models/operations/createtunneledmcpserver.js";
import { DeleteTunneledMcpServerRequest, DeleteTunneledMcpServerSecurity } from "../models/operations/deletetunneledmcpserver.js";
import { GetTunneledMcpServerRequest, GetTunneledMcpServerSecurity } from "../models/operations/gettunneledmcpserver.js";
import { ListTunneledMcpServerConnectionsRequest, ListTunneledMcpServerConnectionsSecurity } from "../models/operations/listtunneledmcpserverconnections.js";
import { ListTunneledMcpServersRequest, ListTunneledMcpServersSecurity } from "../models/operations/listtunneledmcpservers.js";
import { RotateTunneledMcpServerKeyRequest, RotateTunneledMcpServerKeySecurity } from "../models/operations/rotatetunneledmcpserverkey.js";
import { UpdateTunneledMcpServerRequest, UpdateTunneledMcpServerSecurity } from "../models/operations/updatetunneledmcpserver.js";
export declare class TunneledMcp extends ClientSDK {
    /**
     * createServer tunneledMcp
     *
     * @remarks
     * Create a new tunneled MCP server source. Returns the tunnel key once.
     */
    createServer(request: CreateTunneledMcpServerRequest, security?: CreateTunneledMcpServerSecurity | undefined, options?: RequestOptions): Promise<CreateTunneledMcpServerResult>;
    /**
     * deleteServer tunneledMcp
     *
     * @remarks
     * Delete a tunneled MCP server source
     */
    deleteServer(request: DeleteTunneledMcpServerRequest, security?: DeleteTunneledMcpServerSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getServer tunneledMcp
     *
     * @remarks
     * Get a tunneled MCP server by ID
     */
    getServer(request: GetTunneledMcpServerRequest, security?: GetTunneledMcpServerSecurity | undefined, options?: RequestOptions): Promise<TunneledMcpServer>;
    /**
     * listServerConnections tunneledMcp
     *
     * @remarks
     * List live tunnel connections for a tunneled MCP server
     */
    listServerConnections(request: ListTunneledMcpServerConnectionsRequest, security?: ListTunneledMcpServerConnectionsSecurity | undefined, options?: RequestOptions): Promise<TunneledMcpServerConnections>;
    /**
     * listServers tunneledMcp
     *
     * @remarks
     * List all tunneled MCP server sources for a project
     */
    listServers(request?: ListTunneledMcpServersRequest | undefined, security?: ListTunneledMcpServersSecurity | undefined, options?: RequestOptions): Promise<ListTunneledMcpServersResult>;
    /**
     * rotateServerKey tunneledMcp
     *
     * @remarks
     * Rotate a tunneled MCP server source key. Returns the new tunnel key once.
     */
    rotateServerKey(request: RotateTunneledMcpServerKeyRequest, security?: RotateTunneledMcpServerKeySecurity | undefined, options?: RequestOptions): Promise<RotateTunneledMcpServerKeyResult>;
    /**
     * updateServer tunneledMcp
     *
     * @remarks
     * Update a tunneled MCP server source
     */
    updateServer(request: UpdateTunneledMcpServerRequest, security?: UpdateTunneledMcpServerSecurity | undefined, options?: RequestOptions): Promise<TunneledMcpServer>;
}
//# sourceMappingURL=tunneledmcp.d.ts.map