import { Button } from "@speakeasy-api/moonshine";
import { Card } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { ReactNode, Suspense } from "react";
import { ErrorBoundary as ReactErrorBoundary } from "react-error-boundary";
import { handleError } from "@/lib/errors";

interface ContentErrorFallbackProps {
  error: Error;
}

function ContentErrorFallback({ error }: ContentErrorFallbackProps) {
  // Log error to our error handler for consistent logging
  handleError(error, { silent: true });

  return (
    <Card className="w-full max-w-lg m-8 py-8">
      <Card.Header>
        <Card.Title>
          <Stack direction="horizontal" gap={2} align="center">
            <Icon name="circle-alert" className="h-5 w-5 text-destructive" />
            Error loading Page
          </Stack>
        </Card.Title>
      </Card.Header>
      <Card.Content className="space-y-4">
        <Card.Description>
          We encountered an error while loading this page.
        </Card.Description>
        <div className="bg-muted p-3 rounded-md">
          <p className="text-sm text-muted-foreground font-mono">
            {error.message}
          </p>
        </div>
      </Card.Content>
      <Card.Footer className="justify-start">
        <Button variant="secondary" onClick={() => window.location.reload()}>
          <Button.LeftIcon>
            <Icon name="rotate-ccw" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Reload page</Button.Text>
        </Button>
      </Card.Footer>
    </Card>
  );
}

interface ContentErrorBoundaryProps {
  children: ReactNode;
  fallback?: ReactNode;
}

export function ContentErrorBoundary({
  children,
  fallback,
}: ContentErrorBoundaryProps) {
  const defaultFallback = (
    <div className="flex items-center justify-center p-8">
      <Spinner />
    </div>
  );

  return (
    <ReactErrorBoundary
      FallbackComponent={ContentErrorFallback}
      onError={(error, errorInfo) => {
        console.error(
          "Content Error Boundary caught an error:",
          error,
          errorInfo,
        );
      }}
    >
      <Suspense fallback={fallback || defaultFallback}>{children}</Suspense>
    </ReactErrorBoundary>
  );
}
