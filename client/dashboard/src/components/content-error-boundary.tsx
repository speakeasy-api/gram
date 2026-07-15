import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { CircleAlert, RotateCcw } from "lucide-react";
import { ReactNode, Suspense } from "react";
import { ErrorBoundary as ReactErrorBoundary } from "react-error-boundary";
import { handleError, toError } from "@/lib/errors";

interface ContentErrorFallbackProps {
  error: unknown;
}

function ContentErrorFallback({ error: rawError }: ContentErrorFallbackProps) {
  const error = toError(rawError);

  // Log error to our error handler for consistent logging
  handleError(error, { silent: true });

  // Extract request URL from SDK errors (GramError / ServiceError)
  const requestUrl =
    "rawResponse" in error &&
    error.rawResponse instanceof Response &&
    error.rawResponse.url
      ? error.rawResponse.url
      : undefined;

  return (
    <Card className="m-8 w-full max-w-lg py-8">
      <Card.Header>
        <Card.Title>
          <Stack direction="horizontal" gap={2} align="center">
            <CircleAlert className="text-destructive h-5 w-5" />
            Error loading Page
          </Stack>
        </Card.Title>
      </Card.Header>
      <Card.Content className="space-y-4">
        <Card.Description>
          We encountered an error while loading this page.
        </Card.Description>
        <div className="bg-muted p-3">
          <p className="text-muted-foreground font-mono text-sm">
            {error.message}
          </p>
          {requestUrl && (
            <p className="text-muted-foreground mt-2 font-mono text-xs break-all">
              {requestUrl}
            </p>
          )}
        </div>
      </Card.Content>
      <Card.Footer className="justify-start">
        <Button variant="secondary" onClick={() => window.location.reload()}>
          <Button.LeftIcon>
            <RotateCcw className="h-4 w-4" />
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
}: ContentErrorBoundaryProps): JSX.Element {
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
