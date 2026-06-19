import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import type { RemoteSessionIssuer } from "@gram/client/models/components";
import { useListProjects } from "@gram/client/react-query/index.js";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { InfoField, InfoSection, InfoText } from "../../detailFields";

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

export function OverviewTab({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const endpoint = (value: string | undefined): ReactNode => (
    <InfoText mono>{value || "—"}</InfoText>
  );
  const list = (values: string[] | undefined): ReactNode => (
    <InfoText mono>
      {values && values.length > 0 ? values.join(", ") : "—"}
    </InfoText>
  );

  return (
    <div className="max-w-3xl space-y-8">
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
      </InfoSection>
    </div>
  );
}
