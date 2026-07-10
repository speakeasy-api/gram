import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { PluginAssignment } from "./pluginassignment.js";
import { PluginServer } from "./pluginserver.js";
export type Plugin = {
    /**
     * Number of role/user assignments.
     */
    assignmentCount?: number | undefined;
    /**
     * Role/user assignments.
     */
    assignments?: Array<PluginAssignment> | undefined;
    createdAt: Date;
    /**
     * Optional description.
     */
    description?: string | undefined;
    /**
     * Unique plugin identifier.
     */
    id: string;
    /**
     * Display name.
     */
    name: string;
    /**
     * Number of active servers in this plugin.
     */
    serverCount?: number | undefined;
    /**
     * Servers included in this plugin.
     */
    servers?: Array<PluginServer> | undefined;
    /**
     * URL-safe identifier, unique per org.
     */
    slug: string;
    updatedAt: Date;
};
/** @internal */
export declare const Plugin$inboundSchema: z.ZodMiniType<Plugin, unknown>;
export declare function pluginFromJSON(jsonString: string): SafeParseResult<Plugin, SDKValidationError>;
//# sourceMappingURL=plugin.d.ts.map