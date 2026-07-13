import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { MCPRegistry } from "./mcpregistry.js";
export type ListRegistriesResponseBody = {
  /**
   * List of MCP registries
   */
  registries: Array<MCPRegistry>;
};
/** @internal */
export declare const ListRegistriesResponseBody$inboundSchema: z.ZodMiniType<
  ListRegistriesResponseBody,
  unknown
>;
export declare function listRegistriesResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<ListRegistriesResponseBody, SDKValidationError>;
//# sourceMappingURL=listregistriesresponsebody.d.ts.map
