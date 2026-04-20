import { cn } from "@/lib/utils";
import { useAssetImageUploadHandler } from "@/components/useAssetImageUploadHandler";
import { Asset } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { Loader2, UploadIcon } from "lucide-react";
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
        className="group relative w-fit cursor-pointer"
        onClick={() => {
          setAssetId(null);
          onUpload({ id: "" } as Asset);
        }}
      >
        <AssetImage assetId={assetId} className={className} />
        <div className="absolute inset-0 flex items-center justify-center bg-black/50 opacity-0 transition-opacity group-hover:opacity-100">
          <span className="font-medium text-white">Change</span>
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
  isLoading,
}: {
  onUpload: (file: File) => void;
  allowedExtensions?: string[];
  label?: React.ReactNode;
  className?: string;
  isLoading?: boolean;
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
      className={cn("grid w-full max-w-2xl gap-4", className)}
      {...(isLoading ? {} : handlers)}
    >
      <div className="flex w-full items-center justify-center">
        <label
          htmlFor="dropzone-file"
          className={cn(
            "trans flex w-full flex-col items-center justify-center rounded-lg border-1 border-dashed p-10",
            isLoading
              ? "border-primary/50 bg-primary/5 cursor-default"
              : !handlers.isValidFile
                ? "border-destructive bg-destructive/10 cursor-pointer"
                : "border-muted-foreground/50 hover:bg-input/20 cursor-pointer",
          )}
        >
          {isLoading ? (
            <Stack align={"center"} justify={"center"} gap={3}>
              <Loader2 className="text-primary h-5 w-5 animate-spin" />
              <p className="text-card-foreground my-2 text-sm font-semibold">
                Uploading and validating...
              </p>
              <Type small mono muted>
                This may take a few seconds
              </Type>
            </Stack>
          ) : (
            <Stack align={"center"} justify={"center"} gap={3}>
              <UploadIcon className="h-4 w-4" />
              <p className="text-card-foreground my-2 text-sm">
                {label ?? (
                  <>
                    <span className="font-semibold">Click to upload</span> or
                    drag and drop
                  </>
                )}
              </p>
              <Type small mono muted>
                {allowedExtensions?.map((ext) => `.${ext}`)?.join(", ")} (max
                8MiB)
              </Type>
            </Stack>
          )}
          <input
            id="dropzone-file"
            type="file"
            className="hidden"
            onChange={handleFileChange}
            disabled={isLoading}
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

  const { isValidFile, ...handlers } = useFileDropZoneHandlers(
    onUpload,
    allowedExtensions,
  );
  return (
    <label
      htmlFor="dropzone-file"
      tabIndex={0}
      className={cn(
        "inline-flex flex-col items-center justify-center gap-2",
        "cursor-pointer rounded-lg border-1 border-dashed p-6",
        "aspect-square",
        !isValidFile
          ? "border-destructive bg-destructive/10"
          : "border-muted-foreground/50 hover:bg-input/20",
        className,
      )}
      {...handlers}
    >
      {(renderFilePreview && renderFilePreview()) ?? (
        <Stack align={"center"} gap={2}>
          <UploadIcon className="h-4 w-4" />
          <Type mono muted className="text-xs">
            {allowedExtensions?.map((ext) => `.${ext}`)?.join(", ")}
          </Type>
          <Type mono muted className="text-xs">
            (max 8MiB)
          </Type>
        </Stack>
      )}
      <input
        id="dropzone-file"
        type="file"
        className="hidden"
        onChange={handleFileChange}
        accept={allowedExtensions?.map((ext) => `.${ext}`)?.join(",")}
      />
    </label>
  );
}
