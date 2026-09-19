import { Link, useSearchParams } from "react-router";

import { RequireScope } from "@/components/require-scope";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";

import { enterpriseManagedAuthHref } from "./identityProviderQueries";
import { PROVIDERS } from "./providers";

/** EMA is a collection of integrations, not an organization-wide provider choice. */
export function EnterpriseManagedAuth(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <ProviderContent />
    </RequireScope>
  );
}

function ProviderContent(): JSX.Element {
  const [search] = useSearchParams();
  const providerId = search.get("provider");
  if (providerId) {
    const provider = PROVIDERS.find((provider) => provider.id === providerId);
    if (!provider) {
      return (
        <InlineEmptyState
          icon="plug"
          heading="Identity provider not supported"
          description="Choose a supported provider to set up Enterprise Managed Auth."
          action={
            <Button asChild variant="secondary">
              <Link to={enterpriseManagedAuthHref()}>
                View identity providers
              </Link>
            </Button>
          }
        />
      );
    }
    return <provider.Workspace />;
  }
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
