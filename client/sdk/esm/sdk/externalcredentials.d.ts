import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { AwsIamCredential } from "../models/components/awsiamcredential.js";
import { GcpIamCredential } from "../models/components/gcpiamcredential.js";
import { ListExternalCredentialsResult } from "../models/components/listexternalcredentialsresult.js";
import {
  CreateAwsIamCredentialRequest,
  CreateAwsIamCredentialSecurity,
} from "../models/operations/createawsiamcredential.js";
import {
  CreateGcpIamCredentialRequest,
  CreateGcpIamCredentialSecurity,
} from "../models/operations/creategcpiamcredential.js";
import {
  DeleteAwsIamCredentialRequest,
  DeleteAwsIamCredentialSecurity,
} from "../models/operations/deleteawsiamcredential.js";
import {
  DeleteGcpIamCredentialRequest,
  DeleteGcpIamCredentialSecurity,
} from "../models/operations/deletegcpiamcredential.js";
import {
  GetAwsIamCredentialRequest,
  GetAwsIamCredentialSecurity,
} from "../models/operations/getawsiamcredential.js";
import {
  GetGcpIamCredentialRequest,
  GetGcpIamCredentialSecurity,
} from "../models/operations/getgcpiamcredential.js";
import {
  ListAwsIamCredentialsRequest,
  ListAwsIamCredentialsSecurity,
} from "../models/operations/listawsiamcredentials.js";
import {
  ListExternalCredentialsRequest,
  ListExternalCredentialsSecurity,
} from "../models/operations/listexternalcredentials.js";
import {
  ListGcpIamCredentialsRequest,
  ListGcpIamCredentialsSecurity,
} from "../models/operations/listgcpiamcredentials.js";
import {
  UpdateAwsIamCredentialRequest,
  UpdateAwsIamCredentialSecurity,
} from "../models/operations/updateawsiamcredential.js";
import {
  UpdateGcpIamCredentialRequest,
  UpdateGcpIamCredentialSecurity,
} from "../models/operations/updategcpiamcredential.js";
export declare class ExternalCredentials extends ClientSDK {
  /**
   * createAwsIamCredential externalCredentials
   *
   * @remarks
   * Create an AWS IAM external credential. Requires org:admin.
   */
  createAwsIam(
    request: CreateAwsIamCredentialRequest,
    security?: CreateAwsIamCredentialSecurity | undefined,
    options?: RequestOptions,
  ): Promise<AwsIamCredential>;
  /**
   * createGcpIamCredential externalCredentials
   *
   * @remarks
   * Create a GCP IAM external credential. Requires org:admin.
   */
  createGcpIam(
    request: CreateGcpIamCredentialRequest,
    security?: CreateGcpIamCredentialSecurity | undefined,
    options?: RequestOptions,
  ): Promise<GcpIamCredential>;
  /**
   * deleteAwsIamCredential externalCredentials
   *
   * @remarks
   * Soft-delete an AWS IAM external credential by ID. Requires org:admin.
   */
  deleteAwsIam(
    request: DeleteAwsIamCredentialRequest,
    security?: DeleteAwsIamCredentialSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * deleteGcpIamCredential externalCredentials
   *
   * @remarks
   * Soft-delete a GCP IAM external credential by ID. Requires org:admin.
   */
  deleteGcpIam(
    request: DeleteGcpIamCredentialRequest,
    security?: DeleteGcpIamCredentialSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getAwsIamCredential externalCredentials
   *
   * @remarks
   * Get an AWS IAM external credential by ID. Requires org:read.
   */
  getAwsIam(
    request: GetAwsIamCredentialRequest,
    security?: GetAwsIamCredentialSecurity | undefined,
    options?: RequestOptions,
  ): Promise<AwsIamCredential>;
  /**
   * getGcpIamCredential externalCredentials
   *
   * @remarks
   * Get a GCP IAM external credential by ID. Requires org:read.
   */
  getGcpIam(
    request: GetGcpIamCredentialRequest,
    security?: GetGcpIamCredentialSecurity | undefined,
    options?: RequestOptions,
  ): Promise<GcpIamCredential>;
  /**
   * listExternalCredentials externalCredentials
   *
   * @remarks
   * List the organization's external credentials (provider-independent summary). Optionally filter by provider. Requires org:read.
   */
  list(
    request?: ListExternalCredentialsRequest | undefined,
    security?: ListExternalCredentialsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListExternalCredentialsResult>;
  /**
   * listAwsIamCredentials externalCredentials
   *
   * @remarks
   * List the organization's AWS IAM external credentials. Requires org:read.
   */
  listAwsIam(
    request?: ListAwsIamCredentialsRequest | undefined,
    security?: ListAwsIamCredentialsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListExternalCredentialsResult>;
  /**
   * listGcpIamCredentials externalCredentials
   *
   * @remarks
   * List the organization's GCP IAM external credentials. Requires org:read.
   */
  listGcpIam(
    request?: ListGcpIamCredentialsRequest | undefined,
    security?: ListGcpIamCredentialsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListExternalCredentialsResult>;
  /**
   * updateAwsIamCredential externalCredentials
   *
   * @remarks
   * Replace an AWS IAM external credential's configuration. Requires org:admin.
   */
  updateAwsIam(
    request: UpdateAwsIamCredentialRequest,
    security?: UpdateAwsIamCredentialSecurity | undefined,
    options?: RequestOptions,
  ): Promise<AwsIamCredential>;
  /**
   * updateGcpIamCredential externalCredentials
   *
   * @remarks
   * Replace a GCP IAM external credential's configuration. Requires org:admin.
   */
  updateGcpIam(
    request: UpdateGcpIamCredentialRequest,
    security?: UpdateGcpIamCredentialSecurity | undefined,
    options?: RequestOptions,
  ): Promise<GcpIamCredential>;
}
//# sourceMappingURL=externalcredentials.d.ts.map
