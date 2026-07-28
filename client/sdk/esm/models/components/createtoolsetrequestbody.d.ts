import * as z from "zod/v4-mini";
import { ToolsetOrigin, ToolsetOrigin$Outbound } from "./toolsetorigin.js";
export type CreateToolsetRequestBody = {
  /**
   * The slug of the environment to use as the default for the toolset
   */
  defaultEnvironmentSlug?: string | undefined;
  /**
   * Description of the toolset
   */
  description?: string | undefined;
  /**
   * The name of the toolset
   */
  name: string;
  origin?: ToolsetOrigin | undefined;
  /**
   * List of resource URNs to include in the toolset
   */
  resourceUrns?: Array<string> | undefined;
  /**
   * List of tool URNs to include in the toolset
   */
  toolUrns?: Array<string> | undefined;
};
/** @internal */
export type CreateToolsetRequestBody$Outbound = {
  default_environment_slug?: string | undefined;
  description?: string | undefined;
  name: string;
  origin?: ToolsetOrigin$Outbound | undefined;
  resource_urns?: Array<string> | undefined;
  tool_urns?: Array<string> | undefined;
};
/** @internal */
export declare const CreateToolsetRequestBody$outboundSchema: z.ZodMiniType<
  CreateToolsetRequestBody$Outbound,
  CreateToolsetRequestBody
>;
export declare function createToolsetRequestBodyToJSON(
  createToolsetRequestBody: CreateToolsetRequestBody,
): string;
//# sourceMappingURL=createtoolsetrequestbody.d.ts.map
