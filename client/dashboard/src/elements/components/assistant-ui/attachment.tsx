"use client";

import {
  PropsWithChildren,
  useCallback,
  useEffect,
  useState,
  type FC,
} from "react";
import { XIcon, Paperclip, FileText } from "lucide-react";
import {
  AttachmentPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  useAuiState,
  useAui,
} from "@assistant-ui/react";
import { useShallow } from "zustand/shallow";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/elements/components/ui/tooltip";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogTrigger,
} from "@/elements/components/ui/dialog";
import {
  Avatar,
  AvatarImage,
  AvatarFallback,
} from "@/elements/components/ui/avatar";
import { TooltipIconButton } from "@/elements/components/assistant-ui/tooltip-icon-button";
import { getApiUrl } from "@/elements/lib/api";
import { useAuth } from "@/elements/hooks/useAuth";
import { useElements } from "@/elements/hooks/useElements";
import { cn } from "@/lib/utils";
import { attachmentTypeLabel } from "./attachment.helpers";

const useFileSrc = (file: File | undefined) => {
  const [src, setSrc] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!file) {
      setSrc(undefined);
      return;
    }

    const objectUrl = URL.createObjectURL(file);
    setSrc(objectUrl);

    return () => {
      URL.revokeObjectURL(objectUrl);
    };
  }, [file]);

  return src;
};

/**
 * Resolves an image attachment that lives behind Gram's authenticated serve
 * endpoint. A replayed thread has no `File` to make an object URL from, and the
 * stored URL cannot be used as an image source directly because the request
 * needs session headers — so fetch it and hand the preview a blob URL instead.
 */
const useAuthenticatedSrc = (url: string | undefined) => {
  const { config } = useElements();
  const apiUrl = getApiUrl(config);
  const auth = useAuth({ auth: config.api, projectSlug: config.projectSlug });
  const [src, setSrc] = useState<string | undefined>(undefined);
  const ensureValidHeaders = auth.ensureValidHeaders;

  useEffect(() => {
    if (!url || !url.startsWith(`${apiUrl}/rpc/assets.serveChatAttachment`)) {
      setSrc(undefined);
      return;
    }

    let objectUrl: string | undefined;
    let cancelled = false;
    void (async () => {
      try {
        const response = await fetch(url, {
          headers: await ensureValidHeaders(),
        });
        if (!response.ok) return;
        const blob = await response.blob();
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setSrc(objectUrl);
      } catch {
        // Preview is best-effort: the card falls back to its file icon.
      }
    })();

    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [url, apiUrl, ensureValidHeaders]);

  return src;
};

const useAttachmentSrc = () => {
  const { file, src, fileUrl } = useAuiState(
    useShallow(
      ({ attachment }): { file?: File; src?: string; fileUrl?: string } => {
        if (attachment.type !== "image") return {};
        if (attachment.file) return { file: attachment.file };
        const src = attachment.content?.filter((c) => c.type === "image")[0]
          ?.image;
        if (src) return { src };
        // Replayed attachments carry the asset's serve URL on a file part.
        const fileUrl = attachment.content?.filter((c) => c.type === "file")[0]
          ?.data;
        if (!fileUrl) return {};
        return { fileUrl };
      },
    ),
  );

  // Both hooks run unconditionally — `??` would short-circuit the second.
  const objectSrc = useFileSrc(file);
  const authenticatedSrc = useAuthenticatedSrc(fileUrl);
  return objectSrc ?? src ?? authenticatedSrc;
};

type AttachmentPreviewProps = {
  src: string;
};

const AttachmentPreview: FC<AttachmentPreviewProps> = ({ src }) => {
  const [isLoaded, setIsLoaded] = useState(false);
  return (
    <img
      src={src}
      alt="Image Preview"
      className={
        isLoaded
          ? "aui-attachment-preview-image-loaded block h-auto max-h-[80vh] w-auto max-w-full object-contain"
          : "aui-attachment-preview-image-loading hidden"
      }
      onLoad={() => setIsLoaded(true)}
    />
  );
};

const AttachmentPreviewDialog: FC<PropsWithChildren> = ({ children }) => {
  const src = useAttachmentSrc();

  if (!src) return children;

  return (
    <Dialog>
      <DialogTrigger className="aui-attachment-preview-trigger" asChild>
        {children}
      </DialogTrigger>
      <DialogContent className="aui-attachment-preview-dialog-content p-2 sm:max-w-3xl [&_svg]:text-background [&>button]:rounded-full [&>button]:bg-foreground/60 [&>button]:p-1 [&>button]:opacity-100 [&>button]:!ring-0 [&>button]:hover:[&_svg]:text-destructive">
        <DialogTitle className="aui-sr-only sr-only">
          Image Attachment Preview
        </DialogTitle>
        <div className="aui-attachment-preview relative mx-auto flex max-h-[80dvh] w-full items-center justify-center overflow-hidden bg-background">
          <AttachmentPreview src={src} />
        </div>
      </DialogContent>
    </Dialog>
  );
};

/**
 * Opens an attachment that has no inline preview — a PDF, a spec, an audio
 * file. Composer-stage files open straight from the picked `File`; a stored one
 * is fetched with session headers first, because the serve endpoint rejects an
 * unauthenticated `window.open`.
 */
const useOpenAttachment = () => {
  const { config } = useElements();
  const apiUrl = getApiUrl(config);
  const auth = useAuth({ auth: config.api, projectSlug: config.projectSlug });
  const ensureValidHeaders = auth.ensureValidHeaders;
  const { file, fileUrl } = useAuiState(
    useShallow(({ attachment }): { file?: File; fileUrl?: string } => {
      const url = attachment.content?.filter((c) => c.type === "file")[0]?.data;
      return {
        ...(attachment.file ? { file: attachment.file } : {}),
        ...(url ? { fileUrl: url } : {}),
      };
    }),
  );

  const open = useCallback(async () => {
    let objectUrl: string | undefined;
    if (file) {
      objectUrl = URL.createObjectURL(file);
    } else if (fileUrl?.startsWith(apiUrl)) {
      const response = await fetch(fileUrl, {
        headers: await ensureValidHeaders(),
      });
      if (!response.ok) return;
      objectUrl = URL.createObjectURL(await response.blob());
    } else if (fileUrl) {
      window.open(fileUrl, "_blank", "noopener,noreferrer");
      return;
    }
    if (!objectUrl) return;
    const opened = objectUrl;
    window.open(opened, "_blank", "noopener,noreferrer");
    // The new tab has loaded it by now; keeping the URL alive leaks the blob.
    setTimeout(() => URL.revokeObjectURL(opened), 60_000);
  }, [file, fileUrl, apiUrl, ensureValidHeaders]);

  return { open };
};

const AttachmentThumb: FC = () => {
  const isImage = useAuiState(({ attachment }) => attachment.type === "image");
  const src = useAttachmentSrc();

  return (
    <Avatar className="aui-attachment-tile-avatar h-full w-full rounded-none">
      {/* Only once a source resolves: a replayed image is fetched
          asynchronously, and an image element with no source renders as a
          broken-image box instead of the fallback icon. */}
      {src && (
        <AvatarImage
          src={src}
          alt="Attachment preview"
          className="aui-attachment-tile-image object-cover"
        />
      )}
      <AvatarFallback delayMs={isImage ? 200 : 0}>
        <FileText className="aui-attachment-tile-fallback-icon size-8 text-muted-foreground" />
      </AvatarFallback>
    </Avatar>
  );
};

const AttachmentUI: FC = () => {
  const aui = useAui();
  const isComposer = aui.attachment.source === "composer";

  const isImage = useAuiState(({ attachment }) => attachment.type === "image");
  const { open: openAttachment } = useOpenAttachment();
  const typeLabel = useAuiState(({ attachment }) => {
    const type = attachment.type;
    switch (type) {
      case "image":
        return "Image";
      case "document":
        return "Document";
      case "file":
        return "File";
      default:
        return attachmentTypeLabel(type);
    }
  });

  return (
    <Tooltip>
      <AttachmentPrimitive.Root
        className={cn(
          "aui-attachment-root relative",
          isImage &&
            "aui-attachment-root-composer only:[&>#attachment-tile]:size-24",
        )}
      >
        <AttachmentPreviewDialog>
          <TooltipTrigger asChild>
            <div
              className={cn(
                "aui-attachment-tile cursor-pointer overflow-hidden rounded-[14px] border bg-muted transition-opacity hover:opacity-75",
                // An image is its own label; anything else is an icon that
                // only reads as a specific file once the name is on the card.
                isImage
                  ? "size-14"
                  : "aui-attachment-tile-file flex w-36 flex-col",
                isComposer &&
                  "aui-attachment-tile-composer border-foreground/20",
              )}
              role="button"
              id="attachment-tile"
              aria-label={`${typeLabel} attachment`}
              tabIndex={isImage ? undefined : 0}
              onClick={isImage ? undefined : () => void openAttachment()}
              onKeyDown={
                isImage
                  ? undefined
                  : (event) => {
                      if (event.key !== "Enter" && event.key !== " ") return;
                      event.preventDefault();
                      void openAttachment();
                    }
              }
            >
              {isImage ? (
                <AttachmentThumb />
              ) : (
                <>
                  <div className="aui-attachment-tile-file-icon flex h-14 items-center justify-center">
                    <FileText className="size-7 text-muted-foreground" />
                  </div>
                  <div className="aui-attachment-tile-name truncate border-t bg-background/70 px-2 py-1 text-center text-[11px] leading-4 text-muted-foreground">
                    <AttachmentPrimitive.Name />
                  </div>
                </>
              )}
            </div>
          </TooltipTrigger>
        </AttachmentPreviewDialog>
        {isComposer && <AttachmentRemove />}
      </AttachmentPrimitive.Root>
      <TooltipContent side="top">
        <AttachmentPrimitive.Name />
      </TooltipContent>
    </Tooltip>
  );
};

const AttachmentRemove: FC = () => {
  return (
    <AttachmentPrimitive.Remove asChild>
      <TooltipIconButton
        tooltip="Remove file"
        className="aui-attachment-tile-remove absolute top-1.5 right-1.5 size-3.5 rounded-full bg-white text-muted-foreground opacity-100 shadow-sm hover:!bg-white [&_svg]:text-black hover:[&_svg]:text-destructive"
        side="top"
      >
        <XIcon className="aui-attachment-remove-icon size-3 dark:stroke-[2.5px]" />
      </TooltipIconButton>
    </AttachmentPrimitive.Remove>
  );
};

export const UserMessageAttachments: FC = () => {
  return (
    <div className="aui-user-message-attachments-end col-span-full col-start-1 row-start-1 flex w-full flex-row justify-end gap-2">
      <MessagePrimitive.Attachments components={{ Attachment: AttachmentUI }} />
    </div>
  );
};

export const ComposerAttachments: FC = () => {
  return (
    <div className="aui-composer-attachments mb-2 flex w-full flex-row items-center gap-2 overflow-x-auto px-1.5 pt-0.5 pb-1 empty:hidden">
      <ComposerPrimitive.Attachments
        components={{ Attachment: AttachmentUI }}
      />
    </div>
  );
};

export const ComposerAddAttachment: FC = () => {
  return (
    <ComposerPrimitive.AddAttachment asChild>
      <TooltipIconButton
        tooltip="Attach files"
        side="top"
        variant="ghost"
        size="icon"
        align="start"
        className="aui-composer-add-attachment size-[34px] rounded-full p-1 text-xs font-semibold hover:bg-muted-foreground/15 dark:border-muted-foreground/15 dark:hover:bg-muted-foreground/30"
        aria-label="Attach files"
      >
        <Paperclip className="aui-attachment-add-icon size-[18px] stroke-[1.5px]" />
      </TooltipIconButton>
    </ComposerPrimitive.AddAttachment>
  );
};
