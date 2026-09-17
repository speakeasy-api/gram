import { RequireScope } from "@/components/require-scope";
import { UserSessionIssuerSelect } from "@/components/user-session-issuer-select";
import { Button } from "@/components/ui/Button";
import { FieldError } from "@/components/ui/Field";
import { Text } from "@/components/ui/Text";
import { ScopeBadge } from "@/pages/remote-identity-providers/ScopeBadge";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { AuthRow, RowSave } from "./AuthRow";
import type { AuthTarget } from "./authTarget";

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
  const currentIssuerId = target.userSessionIssuerId ?? "";
  const selectedIssuerId = draftIssuerId ?? currentIssuerId;
  const selectedIssuer = issuers.find(
    (issuer) => issuer.id === selectedIssuerId,
  );

  useEffect(() => {
    setDraftIssuerId(null);
  }, [currentIssuerId]);

  const update = useMutation({
    mutationFn: async (userSessionIssuerId: string) => {
      if (!target.linkUserSessionIssuer) {
        throw new Error("This authentication target cannot change issuers");
      }
      await target.linkUserSessionIssuer(userSessionIssuerId);
    },
    onSuccess: async () => {
      setDraftIssuerId(null);
      await Promise.all([
        target.invalidate(queryClient),
        invalidateAllUserSessionIssuer(queryClient, { refetchType: "all" }),
        invalidateAllUserSessionIssuers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("User session issuer updated");
    },
  });

  let control: JSX.Element;
  if (isLoading) {
    control = (
      <Text muted small>
        Loading…
      </Text>
    );
  } else if (isError) {
    control = (
      <FieldError>
        Failed to load user session issuers. Refresh the page to try again.
      </FieldError>
    );
  } else if (issuers.length === 0) {
    control = (
      <Text muted small>
        No existing organization or project issuers are available.
      </Text>
    );
  } else {
    control = (
      <div className="flex flex-wrap items-center gap-2">
        <UserSessionIssuerSelect
          issuers={issuers}
          value={selectedIssuerId}
          onValueChange={setDraftIssuerId}
          disabled={update.isPending}
        />
        {selectedIssuer ? (
          <ScopeBadge
            projectId={selectedIssuer.projectId}
            organizationId={selectedIssuer.organizationId}
          />
        ) : null}
      </div>
    );
  }

  const dirty = draftIssuerId !== null && draftIssuerId !== currentIssuerId;

  return (
    <AuthRow
      label="User session issuer"
      hint="Chooses the authorization policy shared by clients connecting to this server."
    >
      {control}
      {update.isError ? <FieldError>{update.error.message}</FieldError> : null}
      <RowSave visible={dirty}>
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
                if (draftIssuerId) update.mutate(draftIssuerId);
              }}
            >
              {update.isPending ? (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>{update.isPending ? "Saving" : "Save"}</Button.Text>
            </Button>
          )}
        </RequireScope>
      </RowSave>
    </AuthRow>
  );
}
