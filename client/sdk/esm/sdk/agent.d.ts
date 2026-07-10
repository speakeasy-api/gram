import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { GetPluginsResult } from "../models/components/getpluginsresult.js";
import { GetAgentPluginsRequest, GetAgentPluginsSecurity } from "../models/operations/getagentplugins.js";
export declare class Agent extends ClientSDK {
    /**
     * getPlugins agent
     *
     * @remarks
     * Resolve the marketplaces and plugins assigned to the enrolled user. The device agent reconciles these into whichever AI developer tools it manages (Claude Code today), so each tool's own plugin manager fetches and installs the bundles. The response is tool-agnostic: it names what to install, and each tool's syncer decides how to render it into that tool's native configuration.
     */
    getPlugins(request: GetAgentPluginsRequest, security?: GetAgentPluginsSecurity | undefined, options?: RequestOptions): Promise<GetPluginsResult>;
}
//# sourceMappingURL=agent.d.ts.map