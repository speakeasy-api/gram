import { parseAsString, parseAsStringLiteral, useQueryState } from "nuqs";

import { Link } from "react-router";

import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";

import { PROVIDERS } from "./providers";
import { EmaServices } from "./services/EmaServices";
import { EmaServiceWorkspace } from "./services/EmaServiceWorkspace";
import { SpeakeasyProviderCard } from "./SpeakeasyProviderCard";
import { SpeakeasyWorkspace } from "./SpeakeasyWorkspace";
import {
  emaServiceHref,
  oktaViewHref,
  PROVIDER_IDS,
  speakeasyProviderHref,
} from "./tabs";

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
  const [serviceIssuer] = useQueryState("service", parseAsString);
  // A link naming both can't mean either one; ask instead of guessing.
  const ambiguous = !!providerId && !!serviceIssuer;
  const provider =
    showExternal && !ambiguous
      ? PROVIDERS.find((provider) => provider.id === providerId)
      : undefined;
  // Only offer an identity provider the rollout shows; the overview is all a
  // hidden one would open.
  const providerShown =
    providerId === "speakeasy"
      ? showSpeakeasy
      : showExternal && PROVIDERS.some((p) => p.id === providerId);
  if (provider) return <provider.Workspace />;
  if (!ambiguous && providerId === "speakeasy" && showSpeakeasy)
    return <SpeakeasyWorkspace />;
  if (!ambiguous && serviceIssuer)
    return <EmaServiceWorkspace vendorIssuer={serviceIssuer} />;
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
      {ambiguous && (
        <Alert variant="warning">
          <Text small>
            This link points at both an identity provider and a service.{" "}
            {providerShown && (
              <>
                <Link
                  className="underline underline-offset-4"
                  to={
                    providerId === "okta"
                      ? oktaViewHref("setup")
                      : speakeasyProviderHref()
                  }
                >
                  Open the identity provider
                </Link>{" "}
                or{" "}
              </>
            )}
            <Link
              className="underline underline-offset-4"
              to={emaServiceHref(serviceIssuer)}
            >
              {providerShown ? "open the service" : "Open the service"}
            </Link>
            .
          </Text>
        </Alert>
      )}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {showSpeakeasy && <SpeakeasyProviderCard />}
        {showExternal &&
          PROVIDERS.map((provider) => <provider.Card key={provider.id} />)}
      </div>
      <EmaServices />
    </section>
  );
}
