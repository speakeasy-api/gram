import { useState } from "react";
import { UploadIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { Asset, UploadImageResult } from "@gram/client/models/components";
import { useFetcher } from "@/contexts/Fetcher";
import { AssetImage } from "./asset-image";

export function ImageUpload({
  onUpload,
  className,
}: {
  onUpload: (asset: Asset) => void;
  className?: string;
}) {
  const { fetch } = useFetcher();
  const [asset, setAsset] = useState<Asset | null>(null);

  const onImageUpload = async (file: File) => {
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

    setAsset(assetResult.asset);
    onUpload(assetResult.asset);
  };

  if (asset) {
    return <AssetImage assetId={asset.id} className={className} />;
  }

  return (
    <FileUpload
      onUpload={onImageUpload}
      className={className}
      allowedExtensions={["png", "jpg", "jpeg"]}
    />
  );
}

export default function FileUpload({
  onUpload,
  className,
  allowedExtensions,
}: {
  onUpload: (file: File) => void;
  allowedExtensions?: string[];
  className?: string;
}) {
  const [isInvalidFile, setIsInvalidFile] = useState(false);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    console.log("File changed:", e.target.files?.[0]);
    const file = e.target.files?.[0];
    if (file) {
      onUpload(file);
    }
  };

  return (
    <form
      className={cn("grid gap-4 w-full max-w-2xl", className)}
      onDragOver={(e) => {
        e.preventDefault();
        const items = e.dataTransfer.items;
        if (items?.[0]) {
          const fileExtension =
            items[0].type === "application/json" ||
            items[0].type === "application/x-yaml" ||
            items[0].type === "text/yaml";
          setIsInvalidFile(!fileExtension);
        }
      }}
      onDragLeave={() => setIsInvalidFile(false)}
      onDrop={(e) => {
        e.preventDefault();
        setIsInvalidFile(false);
        const droppedFile = e.dataTransfer.files[0];
        if (droppedFile) {
          const fileExtension = droppedFile.name.toLowerCase().split(".").pop();
          if (
            !allowedExtensions ||
            allowedExtensions.includes(fileExtension ?? "")
          ) {
            onUpload(droppedFile);
          } else {
            console.warn(
              `Invalid file type. Please upload one of the following: ${allowedExtensions?.join(
                ", "
              )}`
            );
          }
        }
      }}
    >
      <div className="flex items-center justify-center w-full">
        <label
          htmlFor="dropzone-file"
          className={`flex flex-col items-center justify-center w-full h-64 border-2 border-dashed rounded-lg cursor-pointer bg-card trans ${
            isInvalidFile
              ? "border-destructive bg-destructive/10"
              : "border-muted-foreground/50 hover:bg-input"
          }`}
        >
          <div className="flex flex-col items-center justify-center pt-5 pb-6">
            <UploadIcon className="w-8 h-8 text-muted-foreground" />
            <p className="my-2 text-sm text-card-foreground">
              <span className="font-semibold">Click to upload</span> or drag and
              drop
            </p>
            <p className="text-xs text-muted-foreground">
              {allowedExtensions?.map((ext) => `.${ext}`)?.join(", ")} (max
              8MiB)
            </p>
          </div>
          <input
            id="dropzone-file"
            type="file"
            className="hidden"
            onChange={handleFileChange}
            accept={allowedExtensions?.map((ext) => `.${ext}`)?.join(",")}
          />
        </label>
      </div>
    </form>
  );
}
