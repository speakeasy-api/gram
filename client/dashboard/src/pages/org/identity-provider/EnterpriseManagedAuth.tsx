import { parseAsStringLiteral, useQueryState } from "nuqs";

import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";

import { PROVIDERS } from "./providers";
import { PROVIDER_IDS } from "./tabs";

/** EMA is a collection of integrations, not an organization-wide provider choice. */
export function EnterpriseManagedAuth(): JSX.Element {
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
      aria-label="Enterprise Managed Auth providers"
    >
      <div className="flex flex-col gap-2">
        <Heading variant="h2">Enterprise Managed Auth</Heading>
        <Text muted>
          Use your identity providers to manage how AI agents access company
          applications. Each provider has its own setup and supported features.
        </Text>
        <Text muted small>
          This is separate from employee sign-in and directory sync on the
          Single sign-on tab.
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
