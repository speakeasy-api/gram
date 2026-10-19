import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useCreateOrganizationUserSessionIssuerMutation } from "@gram/client/react-query/createOrganizationUserSessionIssuer.js";
import { invalidateAllOrganizationUserSessionIssuers } from "@gram/client/react-query/organizationUserSessionIssuers.js";

import {
  addSignInIssuerRequest,
  isSignInClientReady,
  normalizeIssuerSlug,
  signInIssuerSlugError,
  type SignInIssuerRow,
} from "./oktaSignIn";

function TrustBadge({ row }: { row: SignInIssuerRow }): JSX.Element {
  if (row.trustsSignIn) {
    return (
      <Badge variant="success" size="sm">
        Trusts Okta sign-in
      </Badge>
    );
  }
  if (row.trustsStale) {
    return (
      <Badge variant="warning" size="sm">
        Trusts a previous agent
      </Badge>
    );
  }
  return (
    <Badge variant="neutral" size="sm">
      {row.trustsOther ? "Trusts another client" : "No sign-in client"}
    </Badge>
  );
}

/** The one place to see and choose which sign-in issuers trust Okta sign-in. */
export function OktaSignInIssuers({
  rows,
  issuers,
  remoteSessionIssuerId,
  client,
  trustDisabled,
  onTrust,
}: {
  rows: SignInIssuerRow[];
  /** Every organization sign-in issuer, across all pages, for slug checks. */
  issuers: UserSessionIssuer[];
  remoteSessionIssuerId: string;
  client: RemoteSessionClient | undefined;
  trustDisabled: boolean;
  onTrust: (row: SignInIssuerRow) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [slug, setSlug] = useState("");
  const readyClient = client && isSignInClientReady(client) ? client : null;
  const slugError = signInIssuerSlugError(slug, issuers);

  const add = useCreateOrganizationUserSessionIssuerMutation({
    onSuccess: (issuer) => {
      toast.success(`Added ${issuer.slug}`);
      setSlug("");
    },
    onSettled: () => invalidateAllOrganizationUserSessionIssuers(queryClient),
  });

  const columns: Column<SignInIssuerRow>[] = [
    {
      key: "slug",
      header: "Sign-in issuer",
      width: "1fr",
      render: (row) => <Text className="font-mono">{row.issuer.slug}</Text>,
    },
    {
      key: "status",
      header: "Okta sign-in",
      width: "200px",
      render: (row) => <TrustBadge row={row} />,
    },
    {
      key: "action",
      header: "",
      width: "180px",
      render: (row) =>
        row.trustsSignIn ? null : (
          <RequireScope scope="org:admin" level="component">
            <Button
              variant="secondary"
              size="sm"
              disabled={trustDisabled || add.isPending}
              onClick={() => onTrust(row)}
            >
              Trust Okta sign-in
            </Button>
          </RequireScope>
        ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <Label>Sign-in issuers</Label>
        <Text small muted>
          Each upstream vendor or authorization server needs its own sign-in
          issuer, because one sign-in issuer serves one upstream.
        </Text>
      </div>
      <Table
        columns={columns}
        data={rows}
        rowKey={(row) => row.issuer.id}
        noResultsMessage={
          <Text small>No organization sign-in issuers yet</Text>
        }
      />
      <RequireScope scope="org:admin" level="component">
        <form
          className="flex items-end gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (!readyClient || slugError) return;
            add.mutate({
              request: addSignInIssuerRequest({
                slug,
                remoteSessionIssuerId,
                clientId: readyClient.id,
              }),
            });
          }}
        >
          <div className="flex flex-1 flex-col gap-1.5">
            <Label htmlFor="okta-sign-in-issuer-slug">
              New sign-in issuer slug
            </Label>
            <Input
              id="okta-sign-in-issuer-slug"
              placeholder="okta-sign-in-linear"
              value={slug}
              error={slug !== "" && !!slugError}
              disabled={!readyClient || add.isPending}
              onChange={(value) => setSlug(normalizeIssuerSlug(value))}
              onBlur={() => setSlug((value) => value.replace(/-+$/, ""))}
            />
          </div>
          <Button
            type="submit"
            variant="secondary"
            disabled={!readyClient || add.isPending || !!slugError}
          >
            {add.isPending ? "Adding…" : "Add sign-in issuer"}
          </Button>
        </form>
      </RequireScope>
      {!readyClient && (
        <Text small muted>
          Set up Okta sign-in before adding more sign-in issuers.
        </Text>
      )}
      {slugError && slug !== "" && (
        <Text small warning>
          {slugError}
        </Text>
      )}
      <ApiErrorAlert error={add.error} />
    </div>
  );
}
