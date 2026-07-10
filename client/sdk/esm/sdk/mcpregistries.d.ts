import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ExternalMCPServer } from "../models/components/externalmcpserver.js";
import { ListCatalogResponseBody } from "../models/components/listcatalogresponsebody.js";
import { ListRegistriesResponseBody } from "../models/components/listregistriesresponsebody.js";
import { ClearMCPRegistryCacheRequest, ClearMCPRegistryCacheSecurity } from "../models/operations/clearmcpregistrycache.js";
import { GetMCPServerDetailsRequest, GetMCPServerDetailsSecurity } from "../models/operations/getmcpserverdetails.js";
import { ListMCPCatalogRequest, ListMCPCatalogSecurity } from "../models/operations/listmcpcatalog.js";
import { ListMCPRegistriesRequest, ListMCPRegistriesSecurity } from "../models/operations/listmcpregistries.js";
export declare class McpRegistries extends ClientSDK {
    /**
     * clearCache mcpRegistries
     *
     * @remarks
     * Clear the registry cache for a specific registry (admin only)
     */
    clearCache(request: ClearMCPRegistryCacheRequest, security?: ClearMCPRegistryCacheSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getServerDetails mcpRegistries
     *
     * @remarks
     * Get detailed information about an MCP server including remotes
     */
    getServerDetails(request: GetMCPServerDetailsRequest, security?: GetMCPServerDetailsSecurity | undefined, options?: RequestOptions): Promise<ExternalMCPServer>;
    /**
     * listCatalog mcpRegistries
     *
     * @remarks
     * List available MCP servers from configured registries
     */
    listCatalog(request?: ListMCPCatalogRequest | undefined, security?: ListMCPCatalogSecurity | undefined, options?: RequestOptions): Promise<ListCatalogResponseBody>;
    /**
     * listRegistries mcpRegistries
     *
     * @remarks
     * List all MCP registries (admin only)
     */
    listRegistries(request?: ListMCPRegistriesRequest | undefined, security?: ListMCPRegistriesSecurity | undefined, options?: RequestOptions): Promise<ListRegistriesResponseBody>;
}
//# sourceMappingURL=mcpregistries.d.ts.map