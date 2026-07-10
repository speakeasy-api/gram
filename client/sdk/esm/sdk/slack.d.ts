import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class Slack extends ClientSDK {
    /**
     * configureSlackApp slack
     *
     * @remarks
     * Store Slack credentials (client ID, client secret, signing secret) for an app.
     */
    configureSlackApp(request: operations.ConfigureSlackAppRequest, security?: operations.ConfigureSlackAppSecurity | undefined, options?: RequestOptions): Promise<components.SlackAppResult>;
    /**
     * createSlackApp slack
     *
     * @remarks
     * Create a new Slack app and generate its manifest.
     */
    createSlackApp(request: operations.CreateSlackAppRequest, security?: operations.CreateSlackAppSecurity | undefined, options?: RequestOptions): Promise<components.CreateSlackAppResult>;
    /**
     * deleteSlackApp slack
     *
     * @remarks
     * Soft-delete a Slack app.
     */
    deleteSlackApp(request: operations.DeleteSlackAppRequest, security?: operations.DeleteSlackAppSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getSlackApp slack
     *
     * @remarks
     * Get details of a specific Slack app.
     */
    getSlackApp(request: operations.GetSlackAppRequest, security?: operations.GetSlackAppSecurity | undefined, options?: RequestOptions): Promise<components.SlackAppResult>;
    /**
     * listSlackApps slack
     *
     * @remarks
     * List Slack apps for a project.
     */
    listSlackApps(request?: operations.ListSlackAppsRequest | undefined, security?: operations.ListSlackAppsSecurity | undefined, options?: RequestOptions): Promise<components.ListSlackAppsResult>;
    /**
     * updateSlackApp slack
     *
     * @remarks
     * Update a Slack app's settings.
     */
    updateSlackApp(request: operations.UpdateSlackAppRequest, security?: operations.UpdateSlackAppSecurity | undefined, options?: RequestOptions): Promise<components.SlackAppResult>;
}
//# sourceMappingURL=slack.d.ts.map