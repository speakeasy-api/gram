import { GramLogo } from "@/components/gram-logo";
import { Stack } from "@/components/ui/Stack";
import { cn } from "@/lib/utils";
import { SpeakeasyWordmark } from "@/pages/login/components/speakeasy-wordmark";

/**
 * Chrome-less full-screen surface for pages a user reaches outside the normal
 * app shell: access requests, policy blocks, fatal errors. Centers its
 * children under the Speakeasy icon, with a subtle escape hatch back home.
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
    <main className="bg-background flex min-h-screen w-full flex-col items-center p-8">
      <div className="flex w-full flex-1 flex-col items-center justify-center">
        <Stack
          gap={8}
          align="center"
          className={cn("w-full max-w-sm", contentClassName)}
        >
          <GramLogo className="w-14" variant="icon" />
          {children}
        </Stack>
      </div>
      <Stack gap={3} align="center" className="mt-auto mb-10 pt-8">
        <SpeakeasyWordmark className="text-muted-foreground h-auto w-32" />
        <a
          href="/"
          className="text-muted-foreground hover:text-foreground text-xs transition-colors"
        >
          Back to home
        </a>
      </Stack>
    </main>
  );
}
