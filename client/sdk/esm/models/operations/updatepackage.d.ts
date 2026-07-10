import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { NotModified } from "../components/notmodified.js";
import {
  UpdatePackageForm,
  UpdatePackageForm$Outbound,
} from "../components/updatepackageform.js";
import { UpdatePackageResult } from "../components/updatepackageresult.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type UpdatePackageSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpdatePackageSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpdatePackageSecurity = {
  option1?: UpdatePackageSecurityOption1 | undefined;
  option2?: UpdatePackageSecurityOption2 | undefined;
};
export type UpdatePackageRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  updatePackageForm: UpdatePackageForm;
};
export type UpdatePackageResponse = UpdatePackageResult | NotModified;
/** @internal */
export type UpdatePackageSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdatePackageSecurityOption1$outboundSchema: z.ZodMiniType<
  UpdatePackageSecurityOption1$Outbound,
  UpdatePackageSecurityOption1
>;
export declare function updatePackageSecurityOption1ToJSON(
  updatePackageSecurityOption1: UpdatePackageSecurityOption1,
): string;
/** @internal */
export type UpdatePackageSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdatePackageSecurityOption2$outboundSchema: z.ZodMiniType<
  UpdatePackageSecurityOption2$Outbound,
  UpdatePackageSecurityOption2
>;
export declare function updatePackageSecurityOption2ToJSON(
  updatePackageSecurityOption2: UpdatePackageSecurityOption2,
): string;
/** @internal */
export type UpdatePackageSecurity$Outbound = {
  Option1?: UpdatePackageSecurityOption1$Outbound | undefined;
  Option2?: UpdatePackageSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdatePackageSecurity$outboundSchema: z.ZodMiniType<
  UpdatePackageSecurity$Outbound,
  UpdatePackageSecurity
>;
export declare function updatePackageSecurityToJSON(
  updatePackageSecurity: UpdatePackageSecurity,
): string;
/** @internal */
export type UpdatePackageRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdatePackageForm: UpdatePackageForm$Outbound;
};
/** @internal */
export declare const UpdatePackageRequest$outboundSchema: z.ZodMiniType<
  UpdatePackageRequest$Outbound,
  UpdatePackageRequest
>;
export declare function updatePackageRequestToJSON(
  updatePackageRequest: UpdatePackageRequest,
): string;
/** @internal */
export declare const UpdatePackageResponse$inboundSchema: z.ZodMiniType<
  UpdatePackageResponse,
  unknown
>;
export declare function updatePackageResponseFromJSON(
  jsonString: string,
): SafeParseResult<UpdatePackageResponse, SDKValidationError>;
//# sourceMappingURL=updatepackage.d.ts.map
