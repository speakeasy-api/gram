import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type DeleteSourceEnvironmentLinkSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
/**
 * The kind of source that can be linked to an environment
 */
export declare const SourceKind: {
  readonly Http: "http";
  readonly Function: "function";
};
/**
 * The kind of source that can be linked to an environment
 */
export type SourceKind = ClosedEnum<typeof SourceKind>;
export type DeleteSourceEnvironmentLinkRequest = {
  /**
   * The kind of source (http or function)
   */
  sourceKind: SourceKind;
  /**
   * The slug of the source
   */
  sourceSlug: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type DeleteSourceEnvironmentLinkSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteSourceEnvironmentLinkSecurity$outboundSchema: z.ZodMiniType<
  DeleteSourceEnvironmentLinkSecurity$Outbound,
  DeleteSourceEnvironmentLinkSecurity
>;
export declare function deleteSourceEnvironmentLinkSecurityToJSON(
  deleteSourceEnvironmentLinkSecurity: DeleteSourceEnvironmentLinkSecurity,
): string;
/** @internal */
export declare const SourceKind$outboundSchema: z.ZodMiniEnum<
  typeof SourceKind
>;
/** @internal */
export type DeleteSourceEnvironmentLinkRequest$Outbound = {
  source_kind: string;
  source_slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteSourceEnvironmentLinkRequest$outboundSchema: z.ZodMiniType<
  DeleteSourceEnvironmentLinkRequest$Outbound,
  DeleteSourceEnvironmentLinkRequest
>;
export declare function deleteSourceEnvironmentLinkRequestToJSON(
  deleteSourceEnvironmentLinkRequest: DeleteSourceEnvironmentLinkRequest,
): string;
//# sourceMappingURL=deletesourceenvironmentlink.d.ts.map
