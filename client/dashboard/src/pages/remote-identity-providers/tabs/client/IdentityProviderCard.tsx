import { Card } from "@/components/ui/Card";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { useRoutes } from "@/routes";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { Link } from "react-router";
import { InfoField, InfoList, InfoText, InfoUrl } from "../../detailFields";
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
      <Card.Header>Identity Provider</Card.Header>
      <Card.Content>
        {issuer ? (
          <div className="grid items-start gap-x-8 gap-y-4 sm:grid-cols-2">
            <InfoField label="Name">
              <Link
                to={routes.remoteIdentityProviders.issuerDetail.overview.href(
                  issuerId,
                )}
                className="hover:text-primary text-sm break-all hover:underline"
              >
                {issuerDisplayName(issuer)}
              </Link>
            </InfoField>
            <InfoField label="Issuer">
              <InfoText mono>{issuer.issuer}</InfoText>
            </InfoField>
            <InfoField label="Authorization Endpoint">
              <InfoUrl value={issuer.authorizationEndpoint} />
            </InfoField>
            <InfoField label="Token Endpoint">
              <InfoUrl value={issuer.tokenEndpoint} />
            </InfoField>
            <InfoField label="Registration Endpoint">
              <InfoUrl value={issuer.registrationEndpoint} />
            </InfoField>
            <InfoField label="Registration Methods">
              <InfoText>
                {methods.length > 0 ? methods.join(", ") : "None advertised"}
              </InfoText>
            </InfoField>
            <InfoField label="Token Endpoint Authentication Methods">
              <InfoList values={issuer.tokenEndpointAuthMethodsSupported} />
            </InfoField>
            <InfoField label="Scopes Supported">
              <InfoList values={issuer.scopesSupported} />
            </InfoField>
          </div>
        ) : (
          <SkeletonParagraph lines={4} />
        )}
      </Card.Content>
    </Card>
  );
}
