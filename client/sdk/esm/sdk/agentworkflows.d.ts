import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class Agentworkflows extends ClientSDK {
    /**
     * createResponse agentworkflows
     *
     * @remarks
     * Create a new agent response. Executes an agent workflow with the provided input and tools.
     */
    createResponse(request: operations.CreateResponseRequest, security?: operations.CreateResponseSecurity | undefined, options?: RequestOptions): Promise<components.WorkflowAgentResponseOutput>;
    /**
     * deleteResponse agentworkflows
     *
     * @remarks
     * Deletes any response associated with a given agent run.
     */
    deleteResponse(request: operations.DeleteResponseRequest, security?: operations.DeleteResponseSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getResponse agentworkflows
     *
     * @remarks
     * Get the status of an async agent response by its ID.
     */
    getResponse(request: operations.GetResponseRequest, security?: operations.GetResponseSecurity | undefined, options?: RequestOptions): Promise<components.WorkflowAgentResponseOutput>;
}
//# sourceMappingURL=agentworkflows.d.ts.map