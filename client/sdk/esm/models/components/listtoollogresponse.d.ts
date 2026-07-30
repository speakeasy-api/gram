import * as z from "zod/v3";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { HTTPToolLog } from "./httptoollog.js";
import { PaginationResponse } from "./paginationresponse.js";
export type ListToolLogResponse = {
  /**
   * Whether tool metrics are enabled for the organization
   */
  enabled: boolean;
  logs: Array<HTTPToolLog>;
  /**
   * Pagination metadata for list responses
   */
  pagination: PaginationResponse;
};
/** @internal */
export declare const ListToolLogResponse$inboundSchema: z.ZodType<
  ListToolLogResponse,
  z.ZodTypeDef,
  unknown
>;
export declare function listToolLogResponseFromJSON(
  jsonString: string,
): SafeParseResult<ListToolLogResponse, SDKValidationError>;
//# sourceMappingURL=listtoollogresponse.d.ts.map
