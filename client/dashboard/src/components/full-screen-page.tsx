import { GramLogo } from "@/components/gram-logo";
import { Stack } from "@/components/ui/Stack";
import { cn } from "@/lib/utils";

/**
 * Chrome-less full-screen surface for pages a user reaches outside the normal
 * app shell: access requests, policy blocks, fatal errors. Centers its
 * children under a vertical Speakeasy logo.
 */
export function FullScreenPage({
  children,
  contentClassName,
}: {
  children: React.ReactNode;
  /** Overrides for the content column, e.g. "max-w-md text-center". */
  contentClassName?: string;
}): JSX.Element {
  return (
    <main className="bg-background flex min-h-screen w-full flex-col items-center justify-center p-8">
      <Stack
        gap={8}
        align="center"
        className={cn("w-full max-w-sm", contentClassName)}
      >
        <GramLogo className="w-25" variant="vertical" />
        {children}
      </Stack>
    </main>
  );
}
