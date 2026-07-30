import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Visibility of the catalog
 */
export declare const PublishRequestBodyVisibility: {
  readonly Public: "public";
  readonly Private: "private";
};
/**
 * Visibility of the catalog
 */
export type PublishRequestBodyVisibility = ClosedEnum<
  typeof PublishRequestBodyVisibility
>;
export type PublishRequestBody = {
  /**
   * Display name for the catalog
   */
  name: string;
  /**
   * URL-friendly identifier for the catalog
   */
  slug: string;
  /**
   * IDs of the toolsets to include
   */
  toolsetIds: Array<string>;
  /**
   * Visibility of the catalog
   */
  visibility?: PublishRequestBodyVisibility | undefined;
};
/** @internal */
export declare const PublishRequestBodyVisibility$outboundSchema: z.ZodMiniEnum<
  typeof PublishRequestBodyVisibility
>;
/** @internal */
export type PublishRequestBody$Outbound = {
  name: string;
  slug: string;
  toolset_ids: Array<string>;
  visibility: string;
};
/** @internal */
export declare const PublishRequestBody$outboundSchema: z.ZodMiniType<
  PublishRequestBody$Outbound,
  PublishRequestBody
>;
export declare function publishRequestBodyToJSON(
  publishRequestBody: PublishRequestBody,
): string;
//# sourceMappingURL=publishrequestbody.d.ts.map
