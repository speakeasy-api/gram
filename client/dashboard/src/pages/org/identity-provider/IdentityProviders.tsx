import { parseAsStringLiteral, useQueryState } from "nuqs";

import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";

import { PROVIDERS } from "./providers";
import { PROVIDER_IDS } from "./tabs";

/** Identity providers are a collection of integrations, not an organization-wide choice. */
export function IdentityProviders(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <ProviderContent />
    </RequireScope>
  );
}

function ProviderContent(): JSX.Element {
  const [providerId] = useQueryState(
    "provider",
    parseAsStringLiteral(PROVIDER_IDS),
  );
  const provider = PROVIDERS.find((provider) => provider.id === providerId);
  if (provider) return <provider.Workspace />;
  return (
    <section
      className="flex min-w-0 flex-col gap-6"
      aria-label="Identity providers"
    >
      <div className="flex flex-col gap-2">
        <Heading variant="h2">Identity providers</Heading>
        <Text muted>
          Connect the identity providers your organization uses. Speakeasy syncs
          their applications and, where a provider supports it, manages how AI
          agents access those applications through Enterprise Managed Auth.
        </Text>
        <Text muted>
          Employee sign-in and directory sync live on the Single sign-on tab.
        </Text>
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {PROVIDERS.map((provider) => (
          <provider.Card key={provider.id} />
        ))}
      </div>
    </section>
  );
}
