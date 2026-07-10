import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListResponseBody } from "../models/components/listresponsebody.js";
import { ListServersResponseBody } from "../models/components/listserversresponsebody.js";
import { MCPCollection } from "../models/components/mcpcollection.js";
import { AttachServerToCollectionRequest, AttachServerToCollectionSecurity } from "../models/operations/attachservertocollection.js";
import { CreateCollectionRequest, CreateCollectionSecurity } from "../models/operations/createcollection.js";
import { DeleteCollectionRequest, DeleteCollectionSecurity } from "../models/operations/deletecollection.js";
import { DetachServerFromCollectionRequest, DetachServerFromCollectionSecurity } from "../models/operations/detachserverfromcollection.js";
import { ListCollectionsRequest, ListCollectionsSecurity } from "../models/operations/listcollections.js";
import { ListCollectionServersRequest, ListCollectionServersSecurity } from "../models/operations/listcollectionservers.js";
import { UpdateCollectionRequest, UpdateCollectionSecurity } from "../models/operations/updatecollection.js";
export declare class Collections extends ClientSDK {
    /**
     * attachServer collections
     *
     * @remarks
     * Attach a server to a collection. Provide exactly one of toolset_id or mcp_server_id.
     */
    attachServer(request: AttachServerToCollectionRequest, security?: AttachServerToCollectionSecurity | undefined, options?: RequestOptions): Promise<MCPCollection>;
    /**
     * create collections
     *
     * @remarks
     * Create an MCP collection within the organization
     */
    create(request: CreateCollectionRequest, security?: CreateCollectionSecurity | undefined, options?: RequestOptions): Promise<MCPCollection>;
    /**
     * delete collections
     *
     * @remarks
     * Delete an MCP collection
     */
    delete(request: DeleteCollectionRequest, security?: DeleteCollectionSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * detachServer collections
     *
     * @remarks
     * Detach a server from a collection. Provide exactly one of toolset_id or mcp_server_id.
     */
    detachServer(request: DetachServerFromCollectionRequest, security?: DetachServerFromCollectionSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * list collections
     *
     * @remarks
     * List MCP collections in the organization
     */
    list(request?: ListCollectionsRequest | undefined, security?: ListCollectionsSecurity | undefined, options?: RequestOptions): Promise<ListResponseBody>;
    /**
     * listServers collections
     *
     * @remarks
     * List published MCP servers from a collection
     */
    listServers(request: ListCollectionServersRequest, security?: ListCollectionServersSecurity | undefined, options?: RequestOptions): Promise<ListServersResponseBody>;
    /**
     * update collections
     *
     * @remarks
     * Update an MCP collection
     */
    update(request: UpdateCollectionRequest, security?: UpdateCollectionSecurity | undefined, options?: RequestOptions): Promise<MCPCollection>;
}
//# sourceMappingURL=collections.d.ts.map