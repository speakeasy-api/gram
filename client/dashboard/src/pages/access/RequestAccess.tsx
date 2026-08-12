import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { useSearchParams, useNavigate } from "react-router";
import { useRequestAccessMutation } from "@gram/client/react-query/requestAccess.js";
import { RequestAccessFormScope } from "@gram/client/models/components/requestaccessform.js";
import { FullScreenPage } from "@/components/full-screen-page";
import React, { useState } from "react";

/**
 * Page for requesting access to a scope. This page is reached via a URL
 * like /request-access?scope=mcp:connect&resource_id=<id>&resource_name=<name>
 *
 * It allows users to send an email request to organization admins.
 */
export default function RequestAccess(): React.JSX.Element {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  const scope = searchParams.get("scope");
  const resourceId = searchParams.get("resource_id") || undefined;
  const resourceName = searchParams.get("resource_name") || undefined;

  const [requestState, setRequestState] = useState<
    "idle" | "sending" | "sent" | "error"
  >("idle");

  const requestAccessMutation = useRequestAccessMutation();

  // Validate that scope is a valid requestable scope
  const validScopes = Object.values(RequestAccessFormScope);
  const isValidScope =
    scope && validScopes.includes(scope as RequestAccessFormScope);

  const handleRequestAccess = async () => {
    if (!isValidScope) return;

    setRequestState("sending");
    try {
      await requestAccessMutation.mutateAsync({
        request: {
          requestAccessForm: {
            scope: scope as RequestAccessFormScope,
            resourceId,
            resourceName,
          },
        },
      });
      setRequestState("sent");
    } catch {
      setRequestState("error");
    }
  };

  const handleGoBack = (): void => {
    void navigate(-1);
  };

  return (
    <FullScreenPage contentClassName="max-w-md">
      <div className="flex w-full flex-col items-center gap-4 text-center">
        <h1 className="text-display-sm font-thin">Request Access</h1>

        {!isValidScope ? (
          <div className="flex flex-col items-center gap-4">
            <p className="text-muted-foreground text-sm">
              Invalid or missing scope parameter. Please use a valid access
              request link.
            </p>
            <Button variant="secondary" size="sm" onClick={handleGoBack}>
              Go Back
            </Button>
          </div>
        ) : (
          <>
            <p className="text-muted-foreground text-sm">
              You don&apos;t have the required permission to access this
              resource. Click the button below to send a request to your
              organization administrators.
            </p>

            {/* Scope info */}
            <div className="bg-muted/25 w-full border px-4 py-3">
              <Stack gap={2}>
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">
                    Requested scope:
                  </span>
                  <span className="font-mono text-xs">{scope}</span>
                </div>
                {resourceName && (
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">Resource:</span>
                    <span>{resourceName}</span>
                  </div>
                )}
              </Stack>
            </div>

            {/* Request button */}
            {requestState === "idle" && (
              <Button
                variant="primary"
                onClick={() => void handleRequestAccess()}
                disabled={requestAccessMutation.isPending}
              >
                {requestAccessMutation.isPending
                  ? "Sending..."
                  : "Send request"}
              </Button>
            )}

            {requestState === "sending" && (
              <p className="text-muted-foreground text-sm">
                Sending request...
              </p>
            )}

            {requestState === "sent" && (
              <div className="flex w-full flex-col items-center gap-3">
                <div className="flex flex-col items-center gap-1">
                  <div className="text-default-success flex items-center gap-1.5 text-sm font-medium">
                    <Icon name="check" className="size-4" />
                    Request sent successfully
                  </div>
                  <p className="text-muted-foreground text-xs">
                    Your organization administrators have been notified. They
                    will review your request and grant access if appropriate.
                  </p>
                </div>
                <Button variant="secondary" size="sm" onClick={handleGoBack}>
                  Go Back
                </Button>
              </div>
            )}

            {requestState === "error" && (
              <div className="flex flex-col items-center gap-3">
                <p className="text-destructive text-sm">
                  Failed to send request. Please try again.
                </p>
                <Stack direction="horizontal" gap={2}>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setRequestState("idle")}
                  >
                    Retry
                  </Button>
                  <Button variant="tertiary" size="sm" onClick={handleGoBack}>
                    Go Back
                  </Button>
                </Stack>
              </div>
            )}
          </>
        )}
      </div>
    </FullScreenPage>
  );
}
