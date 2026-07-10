import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolAnnotations } from "./toolannotations.js";
/**
 * The type of tool
 */
export declare const ToolEntryType: {
  readonly Http: "http";
  readonly Prompt: "prompt";
  readonly Function: "function";
  readonly Platform: "platform";
  readonly Externalmcp: "externalmcp";
};
/**
 * The type of tool
 */
export type ToolEntryType = ClosedEnum<typeof ToolEntryType>;
export type ToolEntry = {
  /**
   * Tool annotations providing behavioral hints about the tool
   */
  annotations?: ToolAnnotations | undefined;
  /**
   * HTTP method for HTTP tools (GET, POST, PUT, PATCH, DELETE)
   */
  httpMethod?: string | undefined;
  /**
   * The ID of the tool
   */
  id: string;
  /**
   * The name of the tool
   */
  name: string;
  /**
   * The URN of the tool
   */
  toolUrn: string;
  /**
   * The type of tool
   */
  type: ToolEntryType;
};
/** @internal */
export declare const ToolEntryType$inboundSchema: z.ZodMiniEnum<
  typeof ToolEntryType
>;
/** @internal */
export declare const ToolEntry$inboundSchema: z.ZodMiniType<ToolEntry, unknown>;
export declare function toolEntryFromJSON(
  jsonString: string,
): SafeParseResult<ToolEntry, SDKValidationError>;
//# sourceMappingURL=toolentry.d.ts.map
