import { useFetcher } from "@/contexts/Fetcher";
import { UploadImageResult } from "@gram/client/models/components";

export function useAssetImageUploadHandler(
  onSuccess: (res: UploadImageResult) => void,
) {
  const { fetch } = useFetcher();

  return async (file: File) => {
    const res = await fetch("/rpc/assets.uploadImage", {
      method: "POST",
      body: file,
      headers: {
        "content-type": file.type,
        "content-length": file.size.toString(),
      },
    });

    if (!res.ok) {
      throw new Error("Upload failed");
    }

    const assetResult: UploadImageResult = await res.json();

    onSuccess(assetResult);
  };
}
