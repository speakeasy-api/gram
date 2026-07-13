import * as z from "zod/v4-mini";
import {
  CreateKeyForm,
  CreateKeyForm$Outbound,
} from "../components/createkeyform.js";
export type CreateAPIKeySecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreateAPIKeyRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createKeyForm: CreateKeyForm;
};
/** @internal */
export type CreateAPIKeySecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateAPIKeySecurity$outboundSchema: z.ZodMiniType<
  CreateAPIKeySecurity$Outbound,
  CreateAPIKeySecurity
>;
export declare function createAPIKeySecurityToJSON(
  createAPIKeySecurity: CreateAPIKeySecurity,
): string;
/** @internal */
export type CreateAPIKeyRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  CreateKeyForm: CreateKeyForm$Outbound;
};
/** @internal */
export declare const CreateAPIKeyRequest$outboundSchema: z.ZodMiniType<
  CreateAPIKeyRequest$Outbound,
  CreateAPIKeyRequest
>;
export declare function createAPIKeyRequestToJSON(
  createAPIKeyRequest: CreateAPIKeyRequest,
): string;
//# sourceMappingURL=createapikey.d.ts.map
