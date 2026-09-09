import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Spinner } from "@/components/ui/Spinner";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { ReactNode, Suspense } from "react";
import { ErrorBoundary as ReactErrorBoundary } from "react-error-boundary";
import { handleError, toError } from "@/lib/errors";
import { useOrgRoutes } from "@/routes";
import { useSlugs } from "@/contexts/Sdk";
import { useMatch } from "react-router";

interface ContentErrorFallbackProps {
  error: unknown;
}

function ContentErrorFallback({ error: rawError }: ContentErrorFallbackProps) {
  const error = toError(rawError);
  const orgRoutes = useOrgRoutes();
  // The boundary also wraps pages rendered outside an organization, where
  // there is no roles page to point at.
  // useSlugs derives a slug from the path, which on /login and other
  // unauthenticated routes is not an organization at all.
  const { orgSlug } = useSlugs();
  // Two segments deep is inside the organization layout; "/login" and the
  // other unauthenticated single-segment routes are not.
  const inOrganization = useMatch("/:orgSlug/:section/*") !== null;

  // Log error to our error handler for consistent logging
  handleError(error, { silent: true });

  // Extract request URL from SDK errors (GramError / ServiceError)
  const requestUrl =
    "rawResponse" in error &&
    error.rawResponse instanceof Response &&
    error.rawResponse.url
      ? error.rawResponse.url
      : undefined;

  // A denial is an answer, not a failure: the permissions behind it are
  // administrable, and the raw message plus a request URL reads as a bug.
  const status =
    "rawResponse" in error && error.rawResponse instanceof Response
      ? error.rawResponse.status
      : undefined;
  const denied = status === 403 || /permission denied/i.test(error.message);

  if (denied) {
    // Same shape as the scope-gated page fallback, so a denial looks the same
    // whether the client knew about it up front or the server said so.
    return (
      <div className="flex h-full min-h-[400px] w-full items-center justify-center">
        <div className="flex max-w-sm flex-col items-center gap-3 text-center">
          <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
            <Icon name="lock" className="text-muted-foreground h-5 w-5" />
          </div>
          <h2 className="text-lg font-medium">Access restricted</h2>
          <p className="text-muted-foreground text-sm">
            You don't have permission to view this. Access is decided by the
            roles you hold and by any rules set on this resource — an
            organization admin can change either.
          </p>
          {orgSlug && inOrganization && (
            <orgRoutes.access.roles.Link>
              <Button variant="secondary" size="sm">
                <Button.Text>Roles & permissions</Button.Text>
              </Button>
            </orgRoutes.access.roles.Link>
          )}
        </div>
      </div>
    );
  }

  return (
    <Card className="m-8 w-full max-w-lg py-8">
      <Card.Header>
        <Card.Title>
          <Stack direction="horizontal" gap={2} align="center">
            <Icon name="circle-alert" className="text-destructive h-5 w-5" />
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
  resetKeys?: unknown[];
}

export function ContentErrorBoundary({
  children,
  fallback,
  resetKeys,
}: ContentErrorBoundaryProps): JSX.Element {
  const defaultFallback = (
    <div className="flex items-center justify-center p-8">
      <Spinner />
    </div>
  );

  return (
    <ReactErrorBoundary
      FallbackComponent={ContentErrorFallback}
      resetKeys={resetKeys}
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
