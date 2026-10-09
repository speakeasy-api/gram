import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import type { ReactNode } from "react";
import { Link } from "react-router";
import {
  InfoField,
  InfoList,
  InfoSection,
  InfoSupported,
  InfoText,
  InfoUrl,
} from "../../detailFields";
import { isAbsoluteHttpUrl } from "../../issuerDocumentationLinks";

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

// NULL and false both read as the default: the issuer's supported scopes are
// requested when a sign-in has no other scope source.
function scopeFallbackLabel(omitScopeFallback: boolean | undefined): string {
  return omitScopeFallback
    ? "Authorization server defaults"
    : "Every advertised scope";
}

export function OverviewTab({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  // Unlike the other metadata arrays, absent and empty mean different things
  // here (mirroring the nullable column): absent is "discovery has not
  // captured this yet", empty is "the issuer advertises no methods".
  const pkceMethods = (values: string[] | null | undefined): ReactNode => {
    if (values == null) {
      return <InfoText>Not captured</InfoText>;
    }
    if (values.length === 0) {
      return <InfoText>None advertised</InfoText>;
    }
    return <InfoText mono>{values.join(", ")}</InfoText>;
  };

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
            <InfoUrl value={issuer.authorizationEndpoint} />
          </InfoField>
          <InfoField label="Token">
            <InfoUrl value={issuer.tokenEndpoint} />
          </InfoField>
          <InfoField label="Registration">
            <InfoUrl value={issuer.registrationEndpoint} />
          </InfoField>
          <InfoField label="JWKS">
            <InfoUrl value={issuer.jwksUri} />
          </InfoField>
        </InfoSection>
      </div>

      <InfoSection title="Identity Provider Details">
        <InfoField label="Scopes">
          <InfoList values={issuer.scopesSupported} />
        </InfoField>
        <InfoField label="Scope Override">
          <InfoList values={issuer.scopeOverride} />
        </InfoField>
        <InfoField label="Scope fallback">
          <InfoText>{scopeFallbackLabel(issuer.omitScopeFallback)}</InfoText>
        </InfoField>
        <InfoField label="Grant Types">
          <InfoList values={issuer.grantTypesSupported} />
        </InfoField>
        <InfoField label="Response Types">
          <InfoList values={issuer.responseTypesSupported} />
        </InfoField>
        <InfoField label="Token Endpoint Authentication Methods">
          <InfoList values={issuer.tokenEndpointAuthMethodsSupported} />
        </InfoField>
        <InfoField label="PKCE Code Challenge Methods">
          {pkceMethods(issuer.codeChallengeMethodsSupported)}
        </InfoField>
        <InfoField label="Client ID Metadata Document">
          <InfoSupported value={issuer.clientIdMetadataDocumentSupported} />
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
