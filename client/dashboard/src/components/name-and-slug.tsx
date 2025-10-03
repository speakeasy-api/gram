import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
import { CopyButton } from "./ui/copy-button";

export const CopyableSlug = ({
  slug,
  hidden = true,
  children,
}: {
  slug: string;
  hidden?: boolean;
  children?: React.ReactNode;
}) => {
  return (
    <Stack direction="horizontal" gap={1} align="center" className="group">
      {children}
      <CopyButton
        text={slug}
        size="icon-sm"
        tooltip="Copy slug"
        className={cn(
          "text-muted-foreground/80 hover:text-foreground",
          hidden && "opacity-0 group-hover:opacity-100",
        )}
      />
    </Stack>
  );
};
