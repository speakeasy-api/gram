import { AssetImage } from "@/components/asset-image";
import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useRef, useState } from "react";
import { toast } from "sonner";
import {
  type AssetUploadTier,
  useAssetImageUploadHandler,
} from "./useAssetImageUploadHandler";

// The image formats assets.uploadImage accepts at every tier. The server
// sniffs the real content type from the bytes; this only filters the picker.
export const IMAGE_UPLOAD_ACCEPT = "image/png,image/jpeg,image/gif,image/webp";

// AssetImageUploadField is the shared logo/icon picker: a preview, an Upload
// button backed by a hidden file input, and a Remove button while a value is
// set. It mirrors the MCP server Branding section's icon control so the two
// branding experiences look the same. The upload stores the asset immediately
// and hands back its id; persisting the reference is the caller's save flow.
export function AssetImageUploadField({
  label = "Logo (optional)",
  description,
  value,
  onChange,
  tier,
  canEdit = true,
  onUploadingChange,
}: {
  label?: string;
  description?: string;
  // The current asset id, empty when unset. onChange receives the uploaded
  // asset's id, or "" when the user removes the logo.
  value: string;
  onChange: (assetId: string) => void;
  tier: AssetUploadTier;
  // When false the preview still renders but the upload/remove controls are
  // hidden, for surfaces that admit read-only viewers.
  canEdit?: boolean;
  // Mirrors the in-flight upload state so the surrounding form can hold its
  // submit until the upload settles. Submitting mid-upload would persist the
  // pre-upload value and silently drop the picked logo.
  onUploadingChange?: (uploading: boolean) => void;
}): JSX.Element {
  const fileInputRef = useRef<HTMLInputElement>(null);
  // Upload/Remove are disabled while an upload is in flight, which also
  // prevents a stale completion from overwriting a newer choice: no newer
  // choice can be made until the current upload settles.
  const [uploading, setUploading] = useState(false);
  const uploadImage = useAssetImageUploadHandler(
    (res) => onChange(res.asset.id),
    tier,
  );

  const setUploadingState = (next: boolean) => {
    setUploading(next);
    onUploadingChange?.(next);
  };

  return (
    <Stack gap={2}>
      <Label className="text-muted-foreground text-xs">{label}</Label>
      <Stack direction="horizontal" gap={3} align="center">
        {value ? (
          <AssetImage assetId={value} alt="" className="size-16 shrink-0" />
        ) : (
          <div className="bg-muted text-muted-foreground flex size-16 shrink-0 items-center justify-center text-xs">
            No icon
          </div>
        )}
        {canEdit && (
          <>
            <input
              ref={fileInputRef}
              type="file"
              accept={IMAGE_UPLOAD_ACCEPT}
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0];
                e.target.value = "";
                if (file) {
                  setUploadingState(true);
                  uploadImage(file)
                    .catch((error: unknown) => {
                      toast.error(
                        error instanceof Error
                          ? error.message
                          : "Failed to upload icon",
                      );
                    })
                    .finally(() => {
                      setUploadingState(false);
                    });
                }
              }}
            />
            <Button
              type="button"
              variant="secondary"
              disabled={uploading}
              onClick={() => fileInputRef.current?.click()}
            >
              <Button.Text>
                {uploading ? "Uploading…" : "Upload icon"}
              </Button.Text>
            </Button>
            {value && (
              <Button
                type="button"
                variant="tertiary"
                disabled={uploading}
                onClick={() => onChange("")}
              >
                <Button.Text>Remove</Button.Text>
              </Button>
            )}
          </>
        )}
      </Stack>
      {description && (
        <Text muted small>
          {description}
        </Text>
      )}
    </Stack>
  );
}
