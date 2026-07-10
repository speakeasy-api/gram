import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The kind of source that can be linked to an environment
 */
export declare const SourceEnvironmentLinkSourceKind: {
  readonly Http: "http";
  readonly Function: "function";
};
/**
 * The kind of source that can be linked to an environment
 */
export type SourceEnvironmentLinkSourceKind = ClosedEnum<
  typeof SourceEnvironmentLinkSourceKind
>;
/**
 * A link between a source and an environment
 */
export type SourceEnvironmentLink = {
  /**
   * The ID of the environment
   */
  environmentId: string;
  /**
   * The ID of the source environment link
   */
  id: string;
  /**
   * The kind of source that can be linked to an environment
   */
  sourceKind: SourceEnvironmentLinkSourceKind;
  /**
   * The slug of the source
   */
  sourceSlug: string;
};
/** @internal */
export declare const SourceEnvironmentLinkSourceKind$inboundSchema: z.ZodMiniEnum<
  typeof SourceEnvironmentLinkSourceKind
>;
/** @internal */
export declare const SourceEnvironmentLink$inboundSchema: z.ZodMiniType<
  SourceEnvironmentLink,
  unknown
>;
export declare function sourceEnvironmentLinkFromJSON(
  jsonString: string,
): SafeParseResult<SourceEnvironmentLink, SDKValidationError>;
//# sourceMappingURL=sourceenvironmentlink.d.ts.map
