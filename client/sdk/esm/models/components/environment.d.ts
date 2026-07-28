import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { EnvironmentEntry } from "./environmententry.js";
/**
 * Model representing an environment
 */
export type Environment = {
  /**
   * The creation date of the environment
   */
  createdAt: Date;
  /**
   * The description of the environment
   */
  description?: string | undefined;
  /**
   * List of environment entries
   */
  entries: Array<EnvironmentEntry>;
  /**
   * The ID of the environment
   */
  id: string;
  /**
   * The name of the environment
   */
  name: string;
  /**
   * The organization ID this environment belongs to
   */
  organizationId: string;
  /**
   * The project ID this environment belongs to
   */
  projectId: string;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
  /**
   * When the environment was last updated
   */
  updatedAt: Date;
};
/** @internal */
export declare const Environment$inboundSchema: z.ZodMiniType<
  Environment,
  unknown
>;
export declare function environmentFromJSON(
  jsonString: string,
): SafeParseResult<Environment, SDKValidationError>;
//# sourceMappingURL=environment.d.ts.map
