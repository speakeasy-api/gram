import { GramLogo } from "@/components/gram-logo/index";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";

interface FullPageErrorProps {
  error: Error;
}

export function FullPageError({ error }: FullPageErrorProps) {
  return (
    <main className="bg-background flex min-h-screen items-center justify-center p-8">
      <Stack gap={6} align="center" className="max-w-md text-center">
        <GramLogo variant="vertical" />

        <Stack gap={3} align="center">
          <Stack direction="horizontal" gap={2} align="center">
            <Icon name="circle-alert" className="text-destructive h-5 w-5" />
            <h2 className="text-lg font-medium">Something went wrong</h2>
          </Stack>

          <p className="text-muted-foreground text-sm">
            An unexpected error occurred. Try reloading the page or contact
            support if the problem persists.
          </p>

          <div className="bg-muted w-full rounded-md p-3">
            <p className="text-muted-foreground font-mono text-xs break-all">
              {error.message}
            </p>
            {"rawResponse" in error &&
              error.rawResponse instanceof Response &&
              error.rawResponse.url && (
                <p className="text-muted-foreground mt-2 font-mono text-xs break-all">
                  {error.rawResponse.url}
                </p>
              )}
          </div>
        </Stack>

        <Stack direction="horizontal" gap={2}>
          <Button variant="brand" onClick={() => window.location.reload()}>
            <Button.LeftIcon>
              <Icon name="rotate-ccw" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Reload page</Button.Text>
          </Button>
          <Button asChild variant="secondary">
            <a href="/">Go to home</a>
          </Button>
        </Stack>
      </Stack>
    </main>
  );
}
