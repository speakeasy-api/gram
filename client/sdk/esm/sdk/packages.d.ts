import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CreatePackageResult } from "../models/components/createpackageresult.js";
import { ListPackagesResult } from "../models/components/listpackagesresult.js";
import { ListVersionsResult } from "../models/components/listversionsresult.js";
import { PublishPackageResult } from "../models/components/publishpackageresult.js";
import {
  CreatePackageRequest,
  CreatePackageSecurity,
} from "../models/operations/createpackage.js";
import {
  ListPackagesRequest,
  ListPackagesSecurity,
} from "../models/operations/listpackages.js";
import {
  ListVersionsRequest,
  ListVersionsSecurity,
} from "../models/operations/listversions.js";
import {
  PublishRequest,
  PublishSecurity,
} from "../models/operations/publish.js";
import {
  UpdatePackageRequest,
  UpdatePackageResponse,
  UpdatePackageSecurity,
} from "../models/operations/updatepackage.js";
export declare class Packages extends ClientSDK {
  /**
   * createPackage packages
   *
   * @remarks
   * Create a new package for a project.
   */
  create(
    request: CreatePackageRequest,
    security?: CreatePackageSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CreatePackageResult>;
  /**
   * listPackages packages
   *
   * @remarks
   * List all packages for a project.
   */
  list(
    request?: ListPackagesRequest | undefined,
    security?: ListPackagesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListPackagesResult>;
  /**
   * listVersions packages
   *
   * @remarks
   * List published versions of a package.
   */
  listVersions(
    request: ListVersionsRequest,
    security?: ListVersionsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListVersionsResult>;
  /**
   * publish packages
   *
   * @remarks
   * Publish a new version of a package.
   */
  publish(
    request: PublishRequest,
    security?: PublishSecurity | undefined,
    options?: RequestOptions,
  ): Promise<PublishPackageResult>;
  /**
   * updatePackage packages
   *
   * @remarks
   * Update package details.
   */
  update(
    request: UpdatePackageRequest,
    security?: UpdatePackageSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UpdatePackageResponse>;
}
//# sourceMappingURL=packages.d.ts.map
