import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Visibility of the collection
 */
export declare const MCPCollectionVisibility: {
  readonly Public: "public";
  readonly Private: "private";
};
/**
 * Visibility of the collection
 */
export type MCPCollectionVisibility = ClosedEnum<
  typeof MCPCollectionVisibility
>;
/**
 * An MCP collection within an organization
 */
export type MCPCollection = {
  /**
   * Description of the collection
   */
  description?: string | undefined;
  /**
   * Collection ID
   */
  id: string;
  /**
   * Registry namespace
   */
  mcpRegistryNamespace?: string | undefined;
  /**
   * Display name for the collection
   */
  name: string;
  /**
   * URL-friendly identifier
   */
  slug: string;
  /**
   * Visibility of the collection
   */
  visibility: MCPCollectionVisibility;
};
/** @internal */
export declare const MCPCollectionVisibility$inboundSchema: z.ZodMiniEnum<
  typeof MCPCollectionVisibility
>;
/** @internal */
export declare const MCPCollection$inboundSchema: z.ZodMiniType<
  MCPCollection,
  unknown
>;
export declare function mcpCollectionFromJSON(
  jsonString: string,
): SafeParseResult<MCPCollection, SDKValidationError>;
//# sourceMappingURL=mcpcollection.d.ts.map
