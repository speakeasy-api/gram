import { GramLogo } from "@/components/gram-logo/index";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";

interface FullPageErrorProps {
  error: Error;
}

export function FullPageError({ error }: FullPageErrorProps) {
  return (
    <main className="flex min-h-screen items-center justify-center bg-background p-8">
      <Stack gap={6} align="center" className="max-w-md text-center">
        <GramLogo variant="vertical" />

        <Stack gap={3} align="center">
          <Stack direction="horizontal" gap={2} align="center">
            <Icon name="circle-alert" className="h-5 w-5 text-destructive" />
            <h2 className="text-lg font-medium">Something went wrong</h2>
          </Stack>

          <p className="text-sm text-muted-foreground">
            An unexpected error occurred. Try reloading the page or contact
            support if the problem persists.
          </p>

          <div className="w-full rounded-md bg-muted p-3">
            <p className="text-xs font-mono text-muted-foreground break-all">
              {error.message}
            </p>
          </div>
        </Stack>

        <Stack direction="horizontal" gap={2}>
          <Button variant="brand" onClick={() => window.location.reload()}>
            <Button.LeftIcon>
              <Icon name="rotate-ccw" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Reload page</Button.Text>
          </Button>
          <Button
            variant="secondary"
            onClick={() => (window.location.href = "/")}
          >
            <Button.Text>Go to home</Button.Text>
          </Button>
        </Stack>
      </Stack>
    </main>
  );
}
