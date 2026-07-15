import { DetailList } from "@/components/ui/detail-list";
import { Heading } from "@/components/ui/heading";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import type { ReactNode } from "react";
import { Link } from "react-router";

// ProjectValue renders the owning project for an issuer: "—" for an
// organizational issuer (no project_id), otherwise the project's slug linked to
// that project. The slug is resolved from the org's project list (the issuer
// record carries only the id).
function ProjectValue({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const { data: projectsData } = useListProjects(
    { organizationId: organization.id },
    undefined,
    { enabled: !!issuer.projectId },
  );

  if (!issuer.projectId) {
    return <>—</>;
  }

  const project = (projectsData?.projects ?? []).find(
    (candidate) => candidate.id === issuer.projectId,
  );

  if (project && orgSlug) {
    return (
      <Link
        to={`/${orgSlug}/projects/${project.slug}`}
        className="hover:text-primary hover:underline"
      >
        {project.slug}
      </Link>
    );
  }

  return <>—</>;
}

function Section({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div>
      <Heading variant="h4" className="mb-3">
        {title}
      </Heading>
      <DetailList orientation="stacked">{children}</DetailList>
    </div>
  );
}

export function OverviewTab({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const endpoint = (value: string | undefined): ReactNode => (
    <span className="font-mono break-all">{value || "—"}</span>
  );
  const list = (values: string[] | undefined): ReactNode => (
    <span className="font-mono break-all">
      {values && values.length > 0 ? values.join(", ") : "—"}
    </span>
  );
  const supported = (value: boolean): ReactNode =>
    value ? "Supported" : "Not supported";

  return (
    <div className="max-w-3xl space-y-8">
      <div className="grid items-start gap-8 sm:grid-cols-2">
        <Section title="Essentials">
          <DetailList.Item label="Name" value={issuer.name || "—"} />
          <DetailList.Item
            label="Slug"
            value={<span className="font-mono break-all">{issuer.slug}</span>}
          />
          <DetailList.Item
            label="Project"
            value={<ProjectValue issuer={issuer} />}
          />
          <DetailList.Item
            label="Issuer"
            value={<span className="font-mono break-all">{issuer.issuer}</span>}
          />
        </Section>

        <Section title="Endpoints">
          <DetailList.Item
            label="Authorization"
            value={endpoint(issuer.authorizationEndpoint)}
          />
          <DetailList.Item
            label="Token"
            value={endpoint(issuer.tokenEndpoint)}
          />
          <DetailList.Item
            label="Registration"
            value={endpoint(issuer.registrationEndpoint)}
          />
          <DetailList.Item label="JWKS" value={endpoint(issuer.jwksUri)} />
        </Section>
      </div>

      <Section title="Identity Provider Details">
        <DetailList.Item label="Scopes" value={list(issuer.scopesSupported)} />
        <DetailList.Item
          label="Grant Types"
          value={list(issuer.grantTypesSupported)}
        />
        <DetailList.Item
          label="Response Types"
          value={list(issuer.responseTypesSupported)}
        />
        <DetailList.Item
          label="Token Endpoint Authentication Methods"
          value={list(issuer.tokenEndpointAuthMethodsSupported)}
        />
        <DetailList.Item
          label="Client ID Metadata Document"
          value={supported(issuer.clientIdMetadataDocumentSupported)}
        />
      </Section>
    </div>
  );
}
