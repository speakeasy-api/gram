import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type Package = {
  /**
   * The creation date of the package
   */
  createdAt: Date;
  /**
   * The deletion date of the package
   */
  deletedAt?: Date | undefined;
  /**
   * The description of the package. This contains HTML content.
   */
  description?: string | undefined;
  /**
   * The unsanitized, user-supplied description of the package. Limited markdown syntax is supported.
   */
  descriptionRaw?: string | undefined;
  /**
   * The ID of the package
   */
  id: string;
  /**
   * The asset ID of the image to show for this package
   */
  imageAssetId?: string | undefined;
  /**
   * The keywords of the package
   */
  keywords?: Array<string> | undefined;
  /**
   * The latest version of the package
   */
  latestVersion?: string | undefined;
  /**
   * The name of the package
   */
  name: string;
  /**
   * The ID of the organization that owns the package
   */
  organizationId: string;
  /**
   * The ID of the project that owns the package
   */
  projectId: string;
  /**
   * The summary of the package
   */
  summary?: string | undefined;
  /**
   * The title of the package
   */
  title?: string | undefined;
  /**
   * The last update date of the package
   */
  updatedAt: Date;
  /**
   * External URL for the package owner
   */
  url?: string | undefined;
};
/** @internal */
export declare const Package$inboundSchema: z.ZodMiniType<Package, unknown>;
export declare function packageFromJSON(
  jsonString: string,
): SafeParseResult<Package, SDKValidationError>;
//# sourceMappingURL=package.d.ts.map
