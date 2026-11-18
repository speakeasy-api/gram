import { RequireScope } from "@/components/require-scope";
import { UserSessionIssuerSelect } from "@/components/user-session-issuer-select";
import { Button } from "@/components/ui/Button";
import { Spinner } from "@/components/ui/Spinner";
import { FieldError } from "@/components/ui/Field";
import { Text } from "@/components/ui/Text";
import { Skeleton } from "@/components/ui/Skeleton";
import { useRoutes } from "@/routes";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Settings } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { AuthRow, RowSave } from "./AuthRow";
import type { AuthTarget } from "./authTarget";
import { ChangeIssuerConfirmDialog } from "./ChangeIssuerConfirmDialog";
import { CONFIG_WRITE_META } from "@/lib/config-write";

export function UserSessionIssuerField({
  target,
  issuers,
  isLoading,
  isError,
}: {
  target: AuthTarget;
  issuers: UserSessionIssuer[];
  isLoading: boolean;
  isError: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [draftIssuerId, setDraftIssuerId] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const currentIssuerId = target.userSessionIssuerId ?? "";
  const selectedIssuerId = draftIssuerId ?? currentIssuerId;
  const selectedIssuer = issuers.find(
    (issuer) => issuer.id === selectedIssuerId,
  );

  useEffect(() => {
    setDraftIssuerId(null);
  }, [currentIssuerId]);

  const update = useMutation({
    meta: CONFIG_WRITE_META,
    mutationFn: async (userSessionIssuerId: string) => {
      if (!target.linkUserSessionIssuer) {
        throw new Error("This authentication target cannot change issuers");
      }
      await target.linkUserSessionIssuer(userSessionIssuerId);
    },
    onSuccess: async () => {
      setDraftIssuerId(null);
      setConfirmOpen(false);
      await Promise.all([
        target.invalidate(queryClient),
        invalidateAllUserSessionIssuer(queryClient, { refetchType: "all" }),
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("MCP Authentication Provider updated");
    },
  });

  let control: JSX.Element;
  if (isLoading) {
    control = (
      <Skeleton label="Loading user session issuers" className="h-9 w-64" />
    );
  } else if (isError) {
    control = (
      <FieldError>
        Failed to load MCP Authentication Providers. Refresh the page to try
        again.
      </FieldError>
    );
  } else if (issuers.length === 0) {
    control = (
      <Text muted small>
        No MCP Authentication Provider is available yet.
      </Text>
    );
  } else {
    control = (
      // A fixed width, so the row doesn't shift as the chosen name changes.
      <div className="flex items-center justify-end gap-2">
        <div className="w-full max-w-md min-w-0 [&_[data-slot=select-value]]:truncate">
          <UserSessionIssuerSelect
            issuers={issuers}
            value={selectedIssuerId}
            onValueChange={setDraftIssuerId}
            disabled={update.isPending}
          />
        </div>
        {currentIssuerId ? (
          // The provider's settings are the organization's to read.
          <RequireScope scope={["org:read", "org:admin"]} level="section">
            <OpenProviderLink issuerId={currentIssuerId} />
          </RequireScope>
        ) : null}
      </div>
    );
  }

  const dirty = draftIssuerId !== null && draftIssuerId !== currentIssuerId;

  return (
    <AuthRow
      label="MCP Authentication Provider"
      hint="Where people sign in to this server. Its settings apply to every server that uses it."
    >
      {control}
      {update.isError && !confirmOpen ? (
        <FieldError>{update.error.message}</FieldError>
      ) : null}
      <RowSave visible={dirty} className="justify-end">
        <RequireScope
          scope="mcp:write"
          resourceId={target.permissionResourceId}
          level="component"
        >
          {({ disabled }) => (
            <Button
              variant="primary"
              size="md"
              disabled={disabled || update.isPending || !draftIssuerId}
              onClick={() => {
                if (!draftIssuerId) return;
                // Every change disturbs connected clients: replacing an issuer
                // signs them out, and a first one starts requiring sign-in.
                update.reset();
                setConfirmOpen(true);
              }}
            >
              {update.isPending ? (
                <Button.LeftIcon>
                  <Spinner className="mr-0" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>{update.isPending ? "Saving" : "Save"}</Button.Text>
            </Button>
          )}
        </RequireScope>
      </RowSave>
      {selectedIssuer && dirty ? (
        <ChangeIssuerConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          target={target}
          replacing={!!currentIssuerId}
          candidate={selectedIssuer}
          onConfirm={() => update.mutate(selectedIssuer.id)}
          isPending={update.isPending}
          error={update.error}
        />
      ) : null}
    </AuthRow>
  );
}

/** Opens the server's MCP Authentication Provider, where its settings live. */
function OpenProviderLink({ issuerId }: { issuerId: string }): JSX.Element {
  const routes = useRoutes();
  return (
    <Button
      variant="secondary"
      size="sm"
      asChild
      aria-label="Open MCP Authentication Provider settings"
    >
      <Link to={routes.mcpSessions.provider.settings.href(issuerId)}>
        <Button.LeftIcon>
          <Settings className="size-4" aria-hidden="true" />
        </Button.LeftIcon>
      </Link>
    </Button>
  );
}
