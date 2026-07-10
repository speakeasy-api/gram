import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListPluginsResult } from "../models/components/listpluginsresult.js";
import { MarketplaceSettingsResult } from "../models/components/marketplacesettingsresult.js";
import { Plugin } from "../models/components/plugin.js";
import { PluginServer } from "../models/components/pluginserver.js";
import { PublishPluginsResult } from "../models/components/publishpluginsresult.js";
import { PublishStatusResult } from "../models/components/publishstatusresult.js";
import { SetPluginAssignmentsResponseBody } from "../models/components/setpluginassignmentsresponsebody.js";
import { UpdateMarketplaceSettingsResult } from "../models/components/updatemarketplacesettingsresult.js";
import { AddPluginServerRequest, AddPluginServerSecurity } from "../models/operations/addpluginserver.js";
import { CreatePluginRequest, CreatePluginSecurity } from "../models/operations/createplugin.js";
import { DeletePluginRequest, DeletePluginSecurity } from "../models/operations/deleteplugin.js";
import { DownloadCodexInstallScriptRequest, DownloadCodexInstallScriptResponse, DownloadCodexInstallScriptSecurity } from "../models/operations/downloadcodexinstallscript.js";
import { DownloadObservabilityPluginRequest, DownloadObservabilityPluginResponse, DownloadObservabilityPluginSecurity } from "../models/operations/downloadobservabilityplugin.js";
import { DownloadPluginPackageRequest, DownloadPluginPackageResponse, DownloadPluginPackageSecurity } from "../models/operations/downloadpluginpackage.js";
import { GetMarketplaceSettingsRequest, GetMarketplaceSettingsSecurity } from "../models/operations/getmarketplacesettings.js";
import { GetPluginRequest, GetPluginSecurity } from "../models/operations/getplugin.js";
import { GetPublishStatusRequest, GetPublishStatusSecurity } from "../models/operations/getpublishstatus.js";
import { ListPluginsRequest, ListPluginsSecurity } from "../models/operations/listplugins.js";
import { PublishPluginsRequest, PublishPluginsSecurity } from "../models/operations/publishplugins.js";
import { RemovePluginServerRequest, RemovePluginServerSecurity } from "../models/operations/removepluginserver.js";
import { SetPluginAssignmentsRequest, SetPluginAssignmentsSecurity } from "../models/operations/setpluginassignments.js";
import { UpdateMarketplaceSettingsRequest, UpdateMarketplaceSettingsSecurity } from "../models/operations/updatemarketplacesettings.js";
import { UpdatePluginRequest, UpdatePluginSecurity } from "../models/operations/updateplugin.js";
import { UpdatePluginServerRequest, UpdatePluginServerSecurity } from "../models/operations/updatepluginserver.js";
export declare class Plugins extends ClientSDK {
    /**
     * addPluginServer plugins
     *
     * @remarks
     * Add an MCP server to a plugin.
     */
    addPluginServer(request: AddPluginServerRequest, security?: AddPluginServerSecurity | undefined, options?: RequestOptions): Promise<PluginServer>;
    /**
     * createPlugin plugins
     *
     * @remarks
     * Create a new plugin.
     */
    createPlugin(request: CreatePluginRequest, security?: CreatePluginSecurity | undefined, options?: RequestOptions): Promise<Plugin>;
    /**
     * deletePlugin plugins
     *
     * @remarks
     * Delete a plugin.
     */
    deletePlugin(request: DeletePluginRequest, security?: DeletePluginSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * downloadCodexInstallScript plugins
     *
     * @remarks
     * Download a bash install script that registers the Codex observability marketplace and pre-approves all hook events. Requires a published marketplace.
     */
    downloadCodexInstallScript(request?: DownloadCodexInstallScriptRequest | undefined, security?: DownloadCodexInstallScriptSecurity | undefined, options?: RequestOptions): Promise<DownloadCodexInstallScriptResponse>;
    /**
     * downloadObservabilityPlugin plugins
     *
     * @remarks
     * Download a ZIP of the per-org observability plugin (Gram hooks). Mints a fresh hooks-scoped API key on each download and embeds it in the plugin's hook script.
     */
    downloadObservabilityPlugin(request: DownloadObservabilityPluginRequest, security?: DownloadObservabilityPluginSecurity | undefined, options?: RequestOptions): Promise<DownloadObservabilityPluginResponse>;
    /**
     * downloadPluginPackage plugins
     *
     * @remarks
     * Download a ZIP of a single plugin package for direct installation.
     */
    downloadPluginPackage(request: DownloadPluginPackageRequest, security?: DownloadPluginPackageSecurity | undefined, options?: RequestOptions): Promise<DownloadPluginPackageResponse>;
    /**
     * getMarketplaceSettings plugins
     *
     * @remarks
     * Get the marketplace settings for the current project, including the effective marketplace name and the server-side default.
     */
    getMarketplaceSettings(request?: GetMarketplaceSettingsRequest | undefined, security?: GetMarketplaceSettingsSecurity | undefined, options?: RequestOptions): Promise<MarketplaceSettingsResult>;
    /**
     * getPlugin plugins
     *
     * @remarks
     * Get a plugin with its servers and assignments.
     */
    getPlugin(request: GetPluginRequest, security?: GetPluginSecurity | undefined, options?: RequestOptions): Promise<Plugin>;
    /**
     * getPublishStatus plugins
     *
     * @remarks
     * Check whether GitHub publishing is configured and connected for this project.
     */
    getPublishStatus(request?: GetPublishStatusRequest | undefined, security?: GetPublishStatusSecurity | undefined, options?: RequestOptions): Promise<PublishStatusResult>;
    /**
     * listPlugins plugins
     *
     * @remarks
     * List all plugins for the current project.
     */
    listPlugins(request?: ListPluginsRequest | undefined, security?: ListPluginsSecurity | undefined, options?: RequestOptions): Promise<ListPluginsResult>;
    /**
     * publishPlugins plugins
     *
     * @remarks
     * Generate and publish all plugin packages to a GitHub repository.
     */
    publishPlugins(request: PublishPluginsRequest, security?: PublishPluginsSecurity | undefined, options?: RequestOptions): Promise<PublishPluginsResult>;
    /**
     * removePluginServer plugins
     *
     * @remarks
     * Remove a server from a plugin.
     */
    removePluginServer(request: RemovePluginServerRequest, security?: RemovePluginServerSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * setPluginAssignments plugins
     *
     * @remarks
     * Replace all assignments for a plugin with the given list of principal URNs.
     */
    setPluginAssignments(request: SetPluginAssignmentsRequest, security?: SetPluginAssignmentsSecurity | undefined, options?: RequestOptions): Promise<SetPluginAssignmentsResponseBody>;
    /**
     * updateMarketplaceSettings plugins
     *
     * @remarks
     * Update the marketplace settings for the current project. If a marketplace is already published, the updated settings are pushed to GitHub before the call returns.
     */
    updateMarketplaceSettings(request: UpdateMarketplaceSettingsRequest, security?: UpdateMarketplaceSettingsSecurity | undefined, options?: RequestOptions): Promise<UpdateMarketplaceSettingsResult>;
    /**
     * updatePlugin plugins
     *
     * @remarks
     * Update plugin metadata.
     */
    updatePlugin(request: UpdatePluginRequest, security?: UpdatePluginSecurity | undefined, options?: RequestOptions): Promise<Plugin>;
    /**
     * updatePluginServer plugins
     *
     * @remarks
     * Update a server's configuration within a plugin.
     */
    updatePluginServer(request: UpdatePluginServerRequest, security?: UpdatePluginServerSecurity | undefined, options?: RequestOptions): Promise<PluginServer>;
}
//# sourceMappingURL=plugins.d.ts.map