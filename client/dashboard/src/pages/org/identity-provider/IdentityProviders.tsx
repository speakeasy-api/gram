import { parseAsStringLiteral, useQueryState } from "nuqs";

import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";

import { PROVIDERS } from "./providers";
import { SpeakeasyProviderCard } from "./SpeakeasyProviderCard";
import { SpeakeasyWorkspace } from "./SpeakeasyWorkspace";
import { PROVIDER_IDS } from "./tabs";

type IdentityProvidersProps = {
  /** External providers (Okta) are behind their own rollout. */
  showExternal: boolean;
  /** Speakeasy's own issuer, once it is offered to the organization. */
  showSpeakeasy: boolean;
};

/** Identity providers are a collection of integrations, not an organization-wide choice. */
export function IdentityProviders(props: IdentityProvidersProps): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <ProviderContent {...props} />
    </RequireScope>
  );
}

function ProviderContent({
  showExternal,
  showSpeakeasy,
}: IdentityProvidersProps): JSX.Element {
  const [providerId] = useQueryState(
    "provider",
    parseAsStringLiteral(PROVIDER_IDS),
  );
  const provider = showExternal
    ? PROVIDERS.find((provider) => provider.id === providerId)
    : undefined;
  if (provider) return <provider.Workspace />;
  if (providerId === "speakeasy" && showSpeakeasy)
    return <SpeakeasyWorkspace />;
  return (
    <section
      className="flex min-w-0 flex-col gap-6"
      aria-label="Identity providers"
    >
      <div className="flex flex-col gap-2">
        <Heading variant="h2">Identity providers</Heading>
        <Text muted>
          Identity providers vouch for your team to the services their AI agents
          use, so each person doesn't authorize every service individually. Use
          Speakeasy itself, or connect the identity providers your organization
          already uses.
        </Text>
        <Text muted>
          Employee sign-in and directory sync live on the Single sign-on tab.
        </Text>
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {showSpeakeasy && <SpeakeasyProviderCard />}
        {showExternal &&
          PROVIDERS.map((provider) => <provider.Card key={provider.id} />)}
      </div>
    </section>
  );
}
