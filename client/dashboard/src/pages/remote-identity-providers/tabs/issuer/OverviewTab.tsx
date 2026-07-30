import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { useOrganizationRemoteSessionClients } from "@gram/client/react-query/organizationRemoteSessionClients.js";
import { useMemo, type ReactNode } from "react";
import { Link } from "react-router";
import { InfoField, InfoSection, InfoText } from "../../detailFields";
import { isAbsoluteHttpUrl } from "../../issuerDocumentationLinks";
import { issuerDisplayName } from "../../issuerDisplay";
import { OAuthFlowDiagram, type IssuerFlowData } from "../../OAuthFlowDiagram";

// ProjectValue renders the owning project for an issuer: "—" for an
// organizational issuer (no project_id), otherwise the project's slug linked to
// that project. The slug is resolved from the org's project list (the issuer
// record carries only the id).
function ProjectValue({ issuer }: { issuer: RemoteSessionIssuer }) {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const { data: projectsData } = useListProjects(
    { organizationId: organization.id },
    undefined,
    { enabled: !!issuer.projectId },
  );

  if (!issuer.projectId) {
    return <InfoText>—</InfoText>;
  }

  const project = (projectsData?.projects ?? []).find(
    (candidate) => candidate.id === issuer.projectId,
  );

  if (project && orgSlug) {
    return (
      <Link
        to={`/${orgSlug}/projects/${project.slug}`}
        className="hover:text-primary text-sm hover:underline"
      >
        {project.slug}
      </Link>
    );
  }

  return <InfoText>—</InfoText>;
}

// DocumentationUrlValue links out to one of the issuer's documentation URLs.
// A value that is not an absolute http(s) URL is shown as plain text rather
// than an href, so an operator can still see (and fix) it without the dashboard
// linking somewhere unsafe.
function DocumentationUrlValue({ value }: { value: string | undefined }) {
  const url = value?.trim();

  if (!url) {
    return <InfoText>—</InfoText>;
  }

  if (!isAbsoluteHttpUrl(url)) {
    return <InfoText mono>{url}</InfoText>;
  }

  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="hover:text-primary text-sm break-all hover:underline"
    >
      {url}
    </a>
  );
}

export function OverviewTab({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  // Fetch clients to aggregate counts for the flow diagram
  const { data: clientsData } = useOrganizationRemoteSessionClients({
    issuerId: issuer.id,
  });

  // Aggregate counts across all clients
  const flowCounts = useMemo(() => {
    const clients = clientsData?.result.items ?? [];
    const clientCount = clients.length;
    const mcpServerCount = clients.reduce(
      (sum, c) => sum + c.mcpServerCount,
      0,
    );
    const sessionCount = clients.reduce(
      (sum, c) => sum + c.activeSessionCount,
      0,
    );
    return { clientCount, mcpServerCount, sessionCount };
  }, [clientsData]);

  const flowData: IssuerFlowData = {
    issuerName: issuerDisplayName(issuer),
    issuerUrl: issuer.issuer,
    clientCount: flowCounts.clientCount,
    mcpServerCount: flowCounts.mcpServerCount,
    sessionCount: flowCounts.sessionCount,
  };

  const endpoint = (value: string | undefined): ReactNode => (
    <InfoText mono>{value || "—"}</InfoText>
  );
  const list = (values: string[] | undefined): ReactNode => (
    <InfoText mono>
      {values && values.length > 0 ? values.join(", ") : "—"}
    </InfoText>
  );
  const supported = (value: boolean): ReactNode => (
    <InfoText>{value ? "Supported" : "Not supported"}</InfoText>
  );

  return (
    <div className="max-w-3xl space-y-8">
      <section>
        <h3 className="text-muted-foreground mb-3 text-xs font-medium tracking-wide uppercase">
          OAuth Flow
        </h3>
        <OAuthFlowDiagram variant="issuer" data={flowData} />
      </section>
      <div className="grid items-start gap-8 sm:grid-cols-2">
        <InfoSection title="Essentials">
          <InfoField label="Name">
            <InfoText>{issuer.name || "—"}</InfoText>
          </InfoField>
          <InfoField label="Slug">
            <InfoText mono>{issuer.slug}</InfoText>
          </InfoField>
          <InfoField label="Project">
            <ProjectValue issuer={issuer} />
          </InfoField>
          <InfoField label="Issuer">
            <InfoText mono>{issuer.issuer}</InfoText>
          </InfoField>
        </InfoSection>

        <InfoSection title="Endpoints">
          <InfoField label="Authorization">
            {endpoint(issuer.authorizationEndpoint)}
          </InfoField>
          <InfoField label="Token">{endpoint(issuer.tokenEndpoint)}</InfoField>
          <InfoField label="Registration">
            {endpoint(issuer.registrationEndpoint)}
          </InfoField>
          <InfoField label="JWKS">{endpoint(issuer.jwksUri)}</InfoField>
        </InfoSection>
      </div>

      <InfoSection title="Identity Provider Details">
        <InfoField label="Scopes">{list(issuer.scopesSupported)}</InfoField>
        <InfoField label="Grant Types">
          {list(issuer.grantTypesSupported)}
        </InfoField>
        <InfoField label="Response Types">
          {list(issuer.responseTypesSupported)}
        </InfoField>
        <InfoField label="Token Endpoint Authentication Methods">
          {list(issuer.tokenEndpointAuthMethodsSupported)}
        </InfoField>
        <InfoField label="Client ID Metadata Document">
          {supported(issuer.clientIdMetadataDocumentSupported)}
        </InfoField>
        <InfoField label="Client Setup Documentation">
          <DocumentationUrlValue value={issuer.clientSetupDocumentationUrl} />
        </InfoField>
        <InfoField label="Service Documentation">
          <DocumentationUrlValue value={issuer.serviceDocumentation} />
        </InfoField>
      </InfoSection>
    </div>
  );
}
