import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class Agents extends ClientSDK {
  /**
   * deleteResponse agents
   *
   * @remarks
   * Deletes any response associated with a given agent run.
   */
  delete(
    request: operations.DeleteAgentResponseRequest,
    security?: operations.DeleteAgentResponseSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getResponse agents
   *
   * @remarks
   * Get the status of an async agent response by its ID.
   */
  get(
    request: operations.GetAgentResponseRequest,
    security?: operations.GetAgentResponseSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.AgentResponseOutput>;
  /**
   * createResponse agents
   *
   * @remarks
   * Create a new agent response. Executes an agent workflow with the provided input and tools.
   */
  create(
    request: operations.CreateAgentResponseRequest,
    security?: operations.CreateAgentResponseSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.AgentResponseOutput>;
}
//# sourceMappingURL=agents.d.ts.map
