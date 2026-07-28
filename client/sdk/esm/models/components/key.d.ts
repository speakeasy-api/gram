import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type Key = {
  /**
   * The creation date of the key.
   */
  createdAt: Date;
  /**
   * The ID of the user who created this key
   */
  createdByUserId: string;
  /**
   * The ID of the key
   */
  id: string;
  /**
   * The token of the api key (only returned on key creation)
   */
  key?: string | undefined;
  /**
   * The store prefix of the api key for recognition
   */
  keyPrefix: string;
  /**
   * When the key was last accessed.
   */
  lastAccessedAt?: Date | undefined;
  /**
   * The name of the key
   */
  name: string;
  /**
   * The organization ID this key belongs to
   */
  organizationId: string;
  /**
   * The optional project ID this key is scoped to
   */
  projectId?: string | undefined;
  /**
   * List of permission scopes for this key
   */
  scopes: Array<string>;
  /**
   * When the key was last updated.
   */
  updatedAt: Date;
};
/** @internal */
export declare const Key$inboundSchema: z.ZodMiniType<Key, unknown>;
export declare function keyFromJSON(
  jsonString: string,
): SafeParseResult<Key, SDKValidationError>;
//# sourceMappingURL=key.d.ts.map
