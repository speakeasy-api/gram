import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type GetSourceEnvironmentSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
/**
 * The kind of source that can be linked to an environment
 */
export declare const QueryParamSourceKind: {
  readonly Http: "http";
  readonly Function: "function";
};
/**
 * The kind of source that can be linked to an environment
 */
export type QueryParamSourceKind = ClosedEnum<typeof QueryParamSourceKind>;
export type GetSourceEnvironmentRequest = {
  /**
   * The kind of source (http or function)
   */
  sourceKind: QueryParamSourceKind;
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
export type GetSourceEnvironmentSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetSourceEnvironmentSecurity$outboundSchema: z.ZodMiniType<
  GetSourceEnvironmentSecurity$Outbound,
  GetSourceEnvironmentSecurity
>;
export declare function getSourceEnvironmentSecurityToJSON(
  getSourceEnvironmentSecurity: GetSourceEnvironmentSecurity,
): string;
/** @internal */
export declare const QueryParamSourceKind$outboundSchema: z.ZodMiniEnum<
  typeof QueryParamSourceKind
>;
/** @internal */
export type GetSourceEnvironmentRequest$Outbound = {
  source_kind: string;
  source_slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetSourceEnvironmentRequest$outboundSchema: z.ZodMiniType<
  GetSourceEnvironmentRequest$Outbound,
  GetSourceEnvironmentRequest
>;
export declare function getSourceEnvironmentRequestToJSON(
  getSourceEnvironmentRequest: GetSourceEnvironmentRequest,
): string;
//# sourceMappingURL=getsourceenvironment.d.ts.map
