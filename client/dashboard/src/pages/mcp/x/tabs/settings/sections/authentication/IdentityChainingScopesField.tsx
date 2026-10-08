import { useId } from "react";
import { Link } from "react-router";

import { MultiSelect } from "@/components/ui/MultiSelect";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";

import {
  chainableClientScopes,
  chainingScopeOptions,
  sanitizeChainingScopes,
} from "./identityChainingScopes";

export function IdentityChainingScopesField({
  client,
  resourceScopes,
  value,
  onChange,
  disabled,
}: {
  client: RemoteSessionClient;
  resourceScopes: string[] | undefined;
  value: string[];
  onChange: (scopes: string[]) => void;
  disabled: boolean;
}): JSX.Element {
  const routes = useRoutes();
  const labelId = useId();
  const unconstrained = chainableClientScopes(client.scope) === undefined;
  const options = chainingScopeOptions(client.scope, resourceScopes, value);

  return (
    <div className="space-y-1.5">
      <Text small className="block font-medium" id={labelId}>
        Requested scopes
      </Text>
      <MultiSelect
        aria-labelledby={labelId}
        options={options}
        value={value}
        onValueChange={(next) => onChange(sanitizeChainingScopes(next))}
        placeholder="No scopes"
        emptyIndicator={
          unconstrained ? "Type a scope to add it." : "No scopes on the client."
        }
        badgeClassName="normal-case tracking-normal"
        maxCount={8}
        creatable={unconstrained}
        caseSensitiveCreate
        hideSelectAll
        disabled={disabled}
      />
      <Text muted small className="block">
        {value.length === 0
          ? "No scope is requested; the identity provider and the server’s authorization server apply their defaults, or may reject the request. "
          : "Sign-in scopes such as openid are never requested. "}
        {unconstrained ? null : (
          <>
            To request another scope, first add it to the{" "}
            <Link
              to={routes.remoteIdentityProviders.clientDetail.settings.href(
                client.remoteSessionIssuerId,
                client.id,
              )}
              className="underline underline-offset-2"
            >
              client’s scope
            </Link>
            .
          </>
        )}
      </Text>
    </div>
  );
}
