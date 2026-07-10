import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CreateProjectResult } from "../models/components/createprojectresult.js";
import { GetProjectResult } from "../models/components/getprojectresult.js";
import { ListAllowedOriginsResult } from "../models/components/listallowedoriginsresult.js";
import { ListProjectsResult } from "../models/components/listprojectsresult.js";
import { SetProjectLogoResult } from "../models/components/setprojectlogoresult.js";
import { UpsertAllowedOriginResult } from "../models/components/upsertallowedoriginresult.js";
import {
  CreateProjectRequest,
  CreateProjectSecurity,
} from "../models/operations/createproject.js";
import {
  DeleteProjectRequest,
  DeleteProjectSecurity,
} from "../models/operations/deleteproject.js";
import {
  GetProjectRequest,
  GetProjectSecurity,
} from "../models/operations/getproject.js";
import {
  ListAllowedOriginsRequest,
  ListAllowedOriginsSecurity,
} from "../models/operations/listallowedorigins.js";
import {
  ListProjectsRequest,
  ListProjectsSecurity,
} from "../models/operations/listprojects.js";
import {
  SetOrganizationWhitelistRequest,
  SetOrganizationWhitelistSecurity,
} from "../models/operations/setorganizationwhitelist.js";
import {
  SetProjectLogoRequest,
  SetProjectLogoSecurity,
} from "../models/operations/setprojectlogo.js";
import {
  UpsertAllowedOriginRequest,
  UpsertAllowedOriginSecurity,
} from "../models/operations/upsertallowedorigin.js";
export declare class Projects extends ClientSDK {
  /**
   * createProject projects
   *
   * @remarks
   * Create a new project.
   */
  create(
    request: CreateProjectRequest,
    security?: CreateProjectSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CreateProjectResult>;
  /**
   * deleteProject projects
   *
   * @remarks
   * Delete a project by its ID
   */
  deleteById(
    request: DeleteProjectRequest,
    security?: DeleteProjectSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getProject projects
   *
   * @remarks
   * Get project details by slug.
   */
  read(
    request: GetProjectRequest,
    security?: GetProjectSecurity | undefined,
    options?: RequestOptions,
  ): Promise<GetProjectResult>;
  /**
   * listProjects projects
   *
   * @remarks
   * List all projects for an organization.
   */
  list(
    request: ListProjectsRequest,
    security?: ListProjectsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListProjectsResult>;
  /**
   * listAllowedOrigins projects
   *
   * @remarks
   * List allowed origins for a project.
   */
  listAllowedOrigins(
    request?: ListAllowedOriginsRequest | undefined,
    security?: ListAllowedOriginsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListAllowedOriginsResult>;
  /**
   * setLogo projects
   *
   * @remarks
   * Uploads a logo for a project.
   */
  setLogo(
    request: SetProjectLogoRequest,
    security?: SetProjectLogoSecurity | undefined,
    options?: RequestOptions,
  ): Promise<SetProjectLogoResult>;
  /**
   * setOrganizationWhitelist projects
   *
   * @remarks
   * Set organization whitelist status (admin only - requires speakeasy-team API key)
   */
  setOrganizationWhitelist(
    request: SetOrganizationWhitelistRequest,
    security?: SetOrganizationWhitelistSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * upsertAllowedOrigin projects
   *
   * @remarks
   * Upsert an allowed origin for a project.
   */
  upsertAllowedOrigin(
    request: UpsertAllowedOriginRequest,
    security?: UpsertAllowedOriginSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UpsertAllowedOriginResult>;
}
//# sourceMappingURL=projects.d.ts.map
