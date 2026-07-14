import * as z from "zod/v4-mini";
import {
  GetSignedAssetURLForm,
  GetSignedAssetURLForm$Outbound,
} from "../components/getsignedasseturlform.js";
export type SetProjectLogoSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type SetProjectLogoSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type SetProjectLogoSecurity = {
  option1?: SetProjectLogoSecurityOption1 | undefined;
  option2?: SetProjectLogoSecurityOption2 | undefined;
};
export type SetProjectLogoRequest = {
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
  getSignedAssetURLForm: GetSignedAssetURLForm;
};
/** @internal */
export type SetProjectLogoSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SetProjectLogoSecurityOption1$outboundSchema: z.ZodMiniType<
  SetProjectLogoSecurityOption1$Outbound,
  SetProjectLogoSecurityOption1
>;
export declare function setProjectLogoSecurityOption1ToJSON(
  setProjectLogoSecurityOption1: SetProjectLogoSecurityOption1,
): string;
/** @internal */
export type SetProjectLogoSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const SetProjectLogoSecurityOption2$outboundSchema: z.ZodMiniType<
  SetProjectLogoSecurityOption2$Outbound,
  SetProjectLogoSecurityOption2
>;
export declare function setProjectLogoSecurityOption2ToJSON(
  setProjectLogoSecurityOption2: SetProjectLogoSecurityOption2,
): string;
/** @internal */
export type SetProjectLogoSecurity$Outbound = {
  Option1?: SetProjectLogoSecurityOption1$Outbound | undefined;
  Option2?: SetProjectLogoSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SetProjectLogoSecurity$outboundSchema: z.ZodMiniType<
  SetProjectLogoSecurity$Outbound,
  SetProjectLogoSecurity
>;
export declare function setProjectLogoSecurityToJSON(
  setProjectLogoSecurity: SetProjectLogoSecurity,
): string;
/** @internal */
export type SetProjectLogoRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  GetSignedAssetURLForm: GetSignedAssetURLForm$Outbound;
};
/** @internal */
export declare const SetProjectLogoRequest$outboundSchema: z.ZodMiniType<
  SetProjectLogoRequest$Outbound,
  SetProjectLogoRequest
>;
export declare function setProjectLogoRequestToJSON(
  setProjectLogoRequest: SetProjectLogoRequest,
): string;
//# sourceMappingURL=setprojectlogo.d.ts.map
