import * as z from "zod/v4-mini";
export type ListPackagesSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListPackagesSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListPackagesSecurity = {
  option1?: ListPackagesSecurityOption1 | undefined;
  option2?: ListPackagesSecurityOption2 | undefined;
};
export type ListPackagesRequest = {
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
};
/** @internal */
export type ListPackagesSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListPackagesSecurityOption1$outboundSchema: z.ZodMiniType<
  ListPackagesSecurityOption1$Outbound,
  ListPackagesSecurityOption1
>;
export declare function listPackagesSecurityOption1ToJSON(
  listPackagesSecurityOption1: ListPackagesSecurityOption1,
): string;
/** @internal */
export type ListPackagesSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListPackagesSecurityOption2$outboundSchema: z.ZodMiniType<
  ListPackagesSecurityOption2$Outbound,
  ListPackagesSecurityOption2
>;
export declare function listPackagesSecurityOption2ToJSON(
  listPackagesSecurityOption2: ListPackagesSecurityOption2,
): string;
/** @internal */
export type ListPackagesSecurity$Outbound = {
  Option1?: ListPackagesSecurityOption1$Outbound | undefined;
  Option2?: ListPackagesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListPackagesSecurity$outboundSchema: z.ZodMiniType<
  ListPackagesSecurity$Outbound,
  ListPackagesSecurity
>;
export declare function listPackagesSecurityToJSON(
  listPackagesSecurity: ListPackagesSecurity,
): string;
/** @internal */
export type ListPackagesRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListPackagesRequest$outboundSchema: z.ZodMiniType<
  ListPackagesRequest$Outbound,
  ListPackagesRequest
>;
export declare function listPackagesRequestToJSON(
  listPackagesRequest: ListPackagesRequest,
): string;
//# sourceMappingURL=listpackages.d.ts.map
