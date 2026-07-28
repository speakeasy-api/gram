import * as z from "zod/v4-mini";
import { EvolveForm, EvolveForm$Outbound } from "../components/evolveform.js";
export type EvolveDeploymentSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type EvolveDeploymentSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type EvolveDeploymentSecurity = {
  option1?: EvolveDeploymentSecurityOption1 | undefined;
  option2?: EvolveDeploymentSecurityOption2 | undefined;
};
export type EvolveDeploymentRequest = {
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
  evolveForm: EvolveForm;
};
/** @internal */
export type EvolveDeploymentSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const EvolveDeploymentSecurityOption1$outboundSchema: z.ZodMiniType<
  EvolveDeploymentSecurityOption1$Outbound,
  EvolveDeploymentSecurityOption1
>;
export declare function evolveDeploymentSecurityOption1ToJSON(
  evolveDeploymentSecurityOption1: EvolveDeploymentSecurityOption1,
): string;
/** @internal */
export type EvolveDeploymentSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const EvolveDeploymentSecurityOption2$outboundSchema: z.ZodMiniType<
  EvolveDeploymentSecurityOption2$Outbound,
  EvolveDeploymentSecurityOption2
>;
export declare function evolveDeploymentSecurityOption2ToJSON(
  evolveDeploymentSecurityOption2: EvolveDeploymentSecurityOption2,
): string;
/** @internal */
export type EvolveDeploymentSecurity$Outbound = {
  Option1?: EvolveDeploymentSecurityOption1$Outbound | undefined;
  Option2?: EvolveDeploymentSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const EvolveDeploymentSecurity$outboundSchema: z.ZodMiniType<
  EvolveDeploymentSecurity$Outbound,
  EvolveDeploymentSecurity
>;
export declare function evolveDeploymentSecurityToJSON(
  evolveDeploymentSecurity: EvolveDeploymentSecurity,
): string;
/** @internal */
export type EvolveDeploymentRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  EvolveForm: EvolveForm$Outbound;
};
/** @internal */
export declare const EvolveDeploymentRequest$outboundSchema: z.ZodMiniType<
  EvolveDeploymentRequest$Outbound,
  EvolveDeploymentRequest
>;
export declare function evolveDeploymentRequestToJSON(
  evolveDeploymentRequest: EvolveDeploymentRequest,
): string;
//# sourceMappingURL=evolvedeployment.d.ts.map
