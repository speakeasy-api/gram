import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { MCPCollection } from "./mcpcollection.js";
export type ListResponseBody = {
  /**
   * List of collections
   */
  collections: Array<MCPCollection>;
};
/** @internal */
export declare const ListResponseBody$inboundSchema: z.ZodMiniType<
  ListResponseBody,
  unknown
>;
export declare function listResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<ListResponseBody, SDKValidationError>;
//# sourceMappingURL=listresponsebody.d.ts.map
