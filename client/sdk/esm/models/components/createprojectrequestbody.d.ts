import * as z from "zod/v4-mini";
export type CreateProjectRequestBody = {
  /**
   * The name of the project
   */
  name: string;
  /**
   * The ID of the organization to create the project in
   */
  organizationId: string;
};
/** @internal */
export type CreateProjectRequestBody$Outbound = {
  name: string;
  organization_id: string;
};
/** @internal */
export declare const CreateProjectRequestBody$outboundSchema: z.ZodMiniType<
  CreateProjectRequestBody$Outbound,
  CreateProjectRequestBody
>;
export declare function createProjectRequestBodyToJSON(
  createProjectRequestBody: CreateProjectRequestBody,
): string;
//# sourceMappingURL=createprojectrequestbody.d.ts.map
