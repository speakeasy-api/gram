import { Card } from "@/components/ui/Card";
import { Link as TextLink } from "@/components/ui/Link";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { IssuerLogo, ScopeBadge, ScopeList } from "@/lib/remote-identity";
import { useRoutes } from "@/routes";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { Link } from "react-router";
import { InfoField, InfoText, InfoUrl, OverflowText } from "../../detailFields";
import { issuerDisplayName } from "../../issuerDisplay";

// registrationMethods names how a client can be registered with the issuer.
function registrationMethods(issuer: RemoteSessionIssuer): string[] {
  const methods: string[] = [];
  if (issuer.registrationEndpoint) methods.push("Dynamic client registration");
  if (issuer.clientIdMetadataDocumentSupported) {
    methods.push("Client ID metadata document");
  }
  return methods;
}

export function IdentityProviderCard({
  issuerId,
  issuer,
}: {
  issuerId: string;
  issuer: RemoteSessionIssuer | undefined;
}): JSX.Element {
  const routes = useRoutes();
  const methods = issuer ? registrationMethods(issuer) : [];

  return (
    <Card>
      <Card.Content>
        {issuer ? (
          <div className="flex flex-col gap-6">
            {/* The provider itself heads the card, ruled off from its
                details edge to edge. */}
            {/* On a narrow card the link wraps below the provider. */}
            <div className="-mx-6 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b px-6 pb-4">
              <div className="flex min-w-0 items-center gap-3">
                <IssuerLogo
                  logoAssetId={issuer.logoAssetId}
                  name={issuerDisplayName(issuer)}
                  size="xl"
                />
                {/* The URL line's hover padding hangs below the text; the
                    negative margin keeps it out of the block's height, so the
                    48px logo spans exactly the name and the URL. */}
                <div className="-mb-1 flex min-w-0 flex-col">
                  <div className="flex min-w-0 items-center gap-2">
                    <Text className="truncate text-base">
                      {issuerDisplayName(issuer)}
                    </Text>
                    <ScopeBadge
                      projectId={issuer.projectId}
                      organizationId={issuer.organizationId}
                    />
                  </div>
                  <OverflowText muted>{issuer.issuer}</OverflowText>
                </div>
              </div>
              <TextLink
                asChild
                variant="secondary"
                size="sm"
                className="visited:text-link-secondary shrink-0"
              >
                <Link
                  to={routes.remoteIdentityProviders.issuerDetail.overview.href(
                    issuerId,
                  )}
                >
                  View identity provider
                </Link>
              </TextLink>
            </div>
            <div className="grid items-start gap-x-8 gap-y-4 sm:grid-cols-2">
              <InfoField label="Authorization endpoint">
                <InfoUrl value={issuer.authorizationEndpoint} />
              </InfoField>
              <InfoField
                label="Token endpoint"
                badges={issuer.tokenEndpointAuthMethodsSupported}
              >
                <InfoUrl value={issuer.tokenEndpoint} />
              </InfoField>
              <InfoField label="Registration endpoint">
                <InfoUrl value={issuer.registrationEndpoint} />
              </InfoField>
              <InfoField label="Registration methods">
                <InfoText>
                  {methods.length > 0 ? methods.join(", ") : "None advertised"}
                </InfoText>
              </InfoField>
              <InfoField label="Scopes supported" className="sm:col-span-2">
                <ScopeList scopes={issuer.scopesSupported} maxLines={3} />
              </InfoField>
            </div>
          </div>
        ) : (
          <SkeletonParagraph lines={4} />
        )}
      </Card.Content>
    </Card>
  );
}
