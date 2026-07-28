import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ProjectEntry = {
  /**
   * The ID of the project
   */
  id: string;
  /**
   * The name of the project
   */
  name: string;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
};
/** @internal */
export declare const ProjectEntry$inboundSchema: z.ZodMiniType<
  ProjectEntry,
  unknown
>;
export declare function projectEntryFromJSON(
  jsonString: string,
): SafeParseResult<ProjectEntry, SDKValidationError>;
//# sourceMappingURL=projectentry.d.ts.map
