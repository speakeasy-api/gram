import { useProject, useSession } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { UploadImageResult } from "@gram/client/models/components/uploadimageresult.js";

// The owning tier of an uploaded image asset. Project uploads go through the
// project-scoped assets service; organization and platform uploads go through
// their tier's own endpoint. The generated SDK functions for the organization
// and platform endpoints do not send the binary request body, and those
// endpoints authorize on the session alone (org:admin / platform admin) and
// reject a project header, so all three tiers upload through this raw-fetch
// handler instead.
export type AssetUploadTier = "project" | "organization" | "platform";

const UPLOAD_ENDPOINTS: Record<AssetUploadTier, string> = {
  project: "/rpc/assets.uploadImage",
  organization: "/rpc/organizationAssets.uploadImage",
  platform: "/rpc/adminAssets.uploadImage",
};

// Matches the server's MaxFileSizeImage cap. Checked client-side so an
// oversized pick fails immediately instead of after an upload round-trip.
export const MAX_IMAGE_UPLOAD_BYTES = 4 * 1024 * 1024;

export function useAssetImageUploadHandler(
  onSuccess: (res: UploadImageResult) => void,
  tier: AssetUploadTier = "project",
): (file: File) => Promise<void> {
  const project = useProject();
  const { session } = useSession();

  return async (file: File) => {
    if (file.size > MAX_IMAGE_UPLOAD_BYTES) {
      throw new Error("Image must be 4 MiB or smaller");
    }

    const headers: Record<string, string> = {
      "content-type": file.type,
      "content-length": file.size.toString(),
      "gram-session": session,
    };
    if (tier === "project") {
      headers["gram-project"] = project.slug;
    }

    const res = await fetch(`${getServerURL()}${UPLOAD_ENDPOINTS[tier]}`, {
      method: "POST",
      body: file,
      headers,
    });

    if (!res.ok) {
      throw new Error("Upload failed");
    }

    const assetResult: UploadImageResult = await res.json();

    onSuccess(assetResult);
  };
}
