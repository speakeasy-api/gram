import { useFetcher } from "@/contexts/Fetcher";
import { cn } from "@/lib/utils";
import { Asset, UploadImageResult } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { UploadIcon } from "lucide-react";
import { useState } from "react";
import { AssetImage } from "./asset-image";
import { Type } from "./ui/type";

export function ImageUpload({
  onUpload,
  existingAssetId,
  className,
}: {
  onUpload: (asset: Asset) => void;
  existingAssetId?: string;
  className?: string;
}) {
  const { fetch } = useFetcher();
  const [assetId, setAssetId] = useState<string | null>(
    existingAssetId ?? null,
  );

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

    setAssetId(assetResult.asset.id);
    onUpload(assetResult.asset);
  };

  if (assetId) {
    return (
      <div
        className="group relative cursor-pointer w-fit"
        onClick={() => {
          setAssetId(null);
          onUpload({ id: "" } as Asset);
        }}
      >
        <AssetImage assetId={assetId} className={className} />
        <div className="absolute inset-0 bg-black/50 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
          <span className="text-white font-medium">Change</span>
        </div>
      </div>
    );
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
  label,
}: {
  onUpload: (file: File) => void;
  allowedExtensions?: string[];
  label?: React.ReactNode;
  className?: string;
}) {
  const [isInvalidFile, setIsInvalidFile] = useState(false);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
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
                ", ",
              )}`,
            );
          }
        }
      }}
    >
      <div className="flex items-center justify-center w-full">
        <label
          htmlFor="dropzone-file"
          className={`flex flex-col items-center justify-center w-full p-10 border-1 border-dashed rounded-lg cursor-pointer trans ${
            isInvalidFile
              ? "border-destructive bg-destructive/10"
              : "border-muted-foreground/50 hover:bg-input/20"
          }`}
        >
          <Stack align={"center"} justify={"center"} gap={3}>
            <UploadIcon className="w-4 h-4" />
            <p className="my-2 text-sm text-card-foreground">
              {label ?? (
                <>
                  <span className="font-semibold">Click to upload</span> or drag
                  and drop
                </>
              )}
            </p>
            <Type small mono muted>
              {allowedExtensions?.map((ext) => `.${ext}`)?.join(", ")} (max
              8MiB)
            </Type>
          </Stack>
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
