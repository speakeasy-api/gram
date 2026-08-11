import { useCallback, useRef, useState, type FC, type ReactNode } from "react";
import { useAui } from "@assistant-ui/react";
import { Paperclip } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * Wraps the thread so files dropped anywhere over it are attached to the
 * composer, the same as picking them with the paperclip.
 *
 * Drag events fire per element, so entering a child element raises `dragleave`
 * on the parent — the depth counter keeps the overlay from flickering as the
 * pointer moves across messages.
 */
export const AttachmentDropZone: FC<{
  children: ReactNode;
  disabled?: boolean;
  className?: string;
}> = ({ children, disabled = false, className }) => {
  const aui = useAui();
  const [isDragging, setIsDragging] = useState(false);
  const depth = useRef(0);

  const hasFiles = (event: React.DragEvent) =>
    event.dataTransfer.types.includes("Files");

  const onDragEnter = useCallback(
    (event: React.DragEvent) => {
      if (disabled || !hasFiles(event)) return;
      depth.current += 1;
      setIsDragging(true);
    },
    [disabled],
  );

  const onDragLeave = useCallback((event: React.DragEvent) => {
    if (!hasFiles(event)) return;
    depth.current = Math.max(0, depth.current - 1);
    if (depth.current === 0) setIsDragging(false);
  }, []);

  const onDragOver = useCallback(
    (event: React.DragEvent) => {
      if (disabled || !hasFiles(event)) return;
      // Required, or the browser opens the file instead of firing `drop`.
      event.preventDefault();
      event.dataTransfer.dropEffect = "copy";
    },
    [disabled],
  );

  const onDrop = useCallback(
    (event: React.DragEvent) => {
      if (disabled || !hasFiles(event)) return;
      event.preventDefault();
      depth.current = 0;
      setIsDragging(false);
      for (const file of Array.from(event.dataTransfer.files)) {
        void aui.composer.addAttachment(file);
      }
    },
    [aui, disabled],
  );

  return (
    <div
      className={cn(
        "aui-attachment-dropzone relative flex min-h-0 flex-1",
        className,
      )}
      onDragEnter={onDragEnter}
      onDragLeave={onDragLeave}
      onDragOver={onDragOver}
      onDrop={onDrop}
    >
      {children}
      {isDragging && (
        <div className="aui-attachment-dropzone-overlay pointer-events-none absolute inset-2 z-50 flex flex-col items-center justify-center gap-2 border-2 border-primary/40 border-dashed bg-background/85 text-muted-foreground">
          <Paperclip className="size-5" />
          <span className="text-sm font-medium">Drop files to attach</span>
        </div>
      )}
    </div>
  );
};
