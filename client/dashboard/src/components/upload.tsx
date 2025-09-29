import { useFetcher } from "@/contexts/Fetcher";
import { cn } from "@/lib/utils";
import { Asset, UploadImageResult } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { UploadIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { AssetImage } from "./asset-image";
import { Type } from "./ui/type";

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

export function ImageUpload({
  onUpload,
  existingAssetId,
  className,
}: {
  onUpload: (asset: Asset) => void;
  existingAssetId?: string;
  className?: string;
}) {
  const [assetId, setAssetId] = useState<string | null>(
    existingAssetId ?? null,
  );

  const handler = useAssetImageUploadHandler((res) => {
    setAssetId(res.asset.id);
    onUpload(res.asset);
  });

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
    <FullWidthUpload
      onUpload={handler}
      className={className}
      allowedExtensions={["png", "jpg", "jpeg"]}
    />
  );
}

interface FileDropzoneHandlers<E extends HTMLElement> {
  isValidFile: boolean;
  onDragOver: (e: React.DragEvent<E>) => void;
  onDragLeave: (e: React.DragEvent<E>) => void;
  onDrop: (e: React.DragEvent<E>) => void;
}

function useFileDropZoneHandlers<E extends HTMLElement>(
  handleFile: (file: File) => void,
  allowedExtensions?: string[],
): FileDropzoneHandlers<E> {
  const [isValidFile, setIsValidFile] = useState<boolean>(true);

  return {
    isValidFile,
    onDragOver: (e: React.DragEvent<E>) => {
      e.preventDefault();
      const items = e.dataTransfer.items;
      if (items?.[0]) {
        const fileExtension =
          items[0].type === "application/json" ||
          items[0].type === "application/x-yaml" ||
          items[0].type === "text/yaml";
        setIsValidFile(!!fileExtension);
      }
    },
    onDragLeave: () => setIsValidFile(true),
    onDrop: (e) => {
      e.preventDefault();
      setIsValidFile(true);
      const droppedFile = e.dataTransfer.files[0];
      if (droppedFile) {
        const fileExtension = droppedFile.name.toLowerCase().split(".").pop();
        if (
          !allowedExtensions ||
          allowedExtensions.includes(fileExtension ?? "")
        ) {
          handleFile(droppedFile);
        } else {
          console.warn(
            `Invalid file type. Please upload one of the following: ${allowedExtensions?.join(
              ", ",
            )}`,
          );
        }
      }
    },
  };
}

export function FullWidthUpload({
  onUpload,
  allowedExtensions,
  className,
  label,
}: {
  onUpload: (file: File) => void;
  allowedExtensions?: string[];
  label?: React.ReactNode;
  className?: string;
}) {
  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      onUpload(file);
    }
  };

  const handlers = useFileDropZoneHandlers(onUpload, allowedExtensions);
  return (
    <div
      tabIndex={0}
      className={cn("grid gap-4 w-full max-w-2xl", className)}
      {...handlers}
    >
      <div className="flex items-center justify-center w-full">
        <label
          htmlFor="dropzone-file"
          className={`flex flex-col items-center justify-center w-full p-10 border-1 border-dashed rounded-lg cursor-pointer trans ${
            !handlers.isValidFile
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
    </div>
  );
}

export function CompactUpload({
  onUpload,
  allowedExtensions,
  className,
  renderFilePreview,
}: {
  onUpload: (file: File) => void;
  allowedExtensions?: string[];
  label?: React.ReactNode;
  className?: string;
  renderFilePreview?: () => React.ReactNode;
}) {
  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      onUpload(file);
    }
  };

  useEffect(() => console.log('file preview', renderFilePreview), [renderFilePreview])
  const handlers = useFileDropZoneHandlers(onUpload, allowedExtensions);
  return (
    <label
      htmlFor="dropzone-file"
      tabIndex={0}
      className={cn(
        "flex flex-col gap-2 items-center justify-center",
        "p-10 border-1 border-dashed rounded-lg cursor-pointer",
        !handlers.isValidFile
          ? "border-destructive bg-destructive/10"
          : "border-muted-foreground/50 hover:bg-input/20",
        className,
      )}
      {...handlers}
    >
      {renderFilePreview ? (
        renderFilePreview()
      ) : (
        <>
          <UploadIcon className="w-4 h-4" />
          <Type small mono muted>
            {allowedExtensions?.map((ext) => `.${ext}`)?.join(", ")} (max 8MiB)
          </Type>
          <input
            id="dropzone-file"
            type="file"
            className="hidden"
            onChange={handleFileChange}
            accept={allowedExtensions?.map((ext) => `.${ext}`)?.join(",")}
          />
        </>
      )}
    </label>
  );
}
