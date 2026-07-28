import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CreateSignedChatAttachmentURLResult } from "../models/components/createsignedchatattachmenturlresult.js";
import { ListAssetsResult } from "../models/components/listassetsresult.js";
import { UploadChatAttachmentResult } from "../models/components/uploadchatattachmentresult.js";
import { UploadFunctionsResult } from "../models/components/uploadfunctionsresult.js";
import { UploadImageResult } from "../models/components/uploadimageresult.js";
import { UploadOpenAPIv3Result } from "../models/components/uploadopenapiv3result.js";
import {
  CreateSignedChatAttachmentURLRequest,
  CreateSignedChatAttachmentURLSecurity,
} from "../models/operations/createsignedchatattachmenturl.js";
import {
  FetchOpenAPIv3FromURLRequest,
  FetchOpenAPIv3FromURLSecurity,
} from "../models/operations/fetchopenapiv3fromurl.js";
import {
  ListAssetsRequest,
  ListAssetsSecurity,
} from "../models/operations/listassets.js";
import {
  ServeChatAttachmentRequest,
  ServeChatAttachmentResponse,
  ServeChatAttachmentSecurity,
} from "../models/operations/servechatattachment.js";
import {
  ServeChatAttachmentSignedRequest,
  ServeChatAttachmentSignedResponse,
} from "../models/operations/servechatattachmentsigned.js";
import {
  ServeFunctionRequest,
  ServeFunctionResponse,
  ServeFunctionSecurity,
} from "../models/operations/servefunction.js";
import {
  ServeImageRequest,
  ServeImageResponse,
} from "../models/operations/serveimage.js";
import {
  ServeOpenAPIv3Request,
  ServeOpenAPIv3Response,
  ServeOpenAPIv3Security,
} from "../models/operations/serveopenapiv3.js";
import {
  UploadChatAttachmentRequest,
  UploadChatAttachmentSecurity,
} from "../models/operations/uploadchatattachment.js";
import {
  UploadFunctionsRequest,
  UploadFunctionsSecurity,
} from "../models/operations/uploadfunctions.js";
import {
  UploadImageRequest,
  UploadImageSecurity,
} from "../models/operations/uploadimage.js";
import {
  UploadOpenAPIv3AssetRequest,
  UploadOpenAPIv3AssetSecurity,
} from "../models/operations/uploadopenapiv3asset.js";
export declare class Assets extends ClientSDK {
  /**
   * createSignedChatAttachmentURL assets
   *
   * @remarks
   * Create a time-limited signed URL to access a chat attachment without authentication.
   */
  createSignedChatAttachmentURL(
    request: CreateSignedChatAttachmentURLRequest,
    security?: CreateSignedChatAttachmentURLSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CreateSignedChatAttachmentURLResult>;
  /**
   * fetchOpenAPIv3FromURL assets
   *
   * @remarks
   * Fetch an OpenAPI v3 document from a URL and upload it to Gram.
   */
  fetchOpenAPIv3FromURL(
    request: FetchOpenAPIv3FromURLRequest,
    security?: FetchOpenAPIv3FromURLSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UploadOpenAPIv3Result>;
  /**
   * listAssets assets
   *
   * @remarks
   * List all assets for a project.
   */
  listAssets(
    request?: ListAssetsRequest | undefined,
    security?: ListAssetsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListAssetsResult>;
  /**
   * serveChatAttachment assets
   *
   * @remarks
   * Serve a chat attachment from Gram.
   */
  serveChatAttachment(
    request: ServeChatAttachmentRequest,
    security?: ServeChatAttachmentSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ServeChatAttachmentResponse>;
  /**
   * serveChatAttachmentSigned assets
   *
   * @remarks
   * Serve a chat attachment using a signed URL token.
   */
  serveChatAttachmentSigned(
    request: ServeChatAttachmentSignedRequest,
    options?: RequestOptions,
  ): Promise<ServeChatAttachmentSignedResponse>;
  /**
   * serveFunction assets
   *
   * @remarks
   * Serve a Gram Functions asset from Gram.
   */
  serveFunction(
    request: ServeFunctionRequest,
    security?: ServeFunctionSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ServeFunctionResponse>;
  /**
   * serveImage assets
   *
   * @remarks
   * Serve an image from Gram.
   */
  serveImage(
    request: ServeImageRequest,
    options?: RequestOptions,
  ): Promise<ServeImageResponse>;
  /**
   * serveOpenAPIv3 assets
   *
   * @remarks
   * Serve an OpenAPIv3 asset from Gram.
   */
  serveOpenAPIv3(
    request: ServeOpenAPIv3Request,
    security?: ServeOpenAPIv3Security | undefined,
    options?: RequestOptions,
  ): Promise<ServeOpenAPIv3Response>;
  /**
   * uploadChatAttachment assets
   *
   * @remarks
   * Upload a chat attachment to Gram.
   */
  uploadChatAttachment(
    request: UploadChatAttachmentRequest,
    security?: UploadChatAttachmentSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UploadChatAttachmentResult>;
  /**
   * uploadFunctions assets
   *
   * @remarks
   * Upload functions to Gram.
   */
  uploadFunctions(
    request: UploadFunctionsRequest,
    security?: UploadFunctionsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UploadFunctionsResult>;
  /**
   * uploadImage assets
   *
   * @remarks
   * Upload an image to Gram.
   */
  uploadImage(
    request: UploadImageRequest,
    security?: UploadImageSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UploadImageResult>;
  /**
   * uploadOpenAPIv3 assets
   *
   * @remarks
   * Upload an OpenAPI v3 document to Gram.
   */
  uploadOpenAPIv3(
    request: UploadOpenAPIv3AssetRequest,
    security?: UploadOpenAPIv3AssetSecurity | undefined,
    options?: RequestOptions,
  ): Promise<UploadOpenAPIv3Result>;
}
//# sourceMappingURL=assets.d.ts.map
