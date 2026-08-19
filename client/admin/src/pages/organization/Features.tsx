import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import type { JSX, ReactNode } from "react";

import { organizationFeaturesQuery, organizationQuery } from "@/lib/adminQueries";
import type { AdminOrganization, AdminOrganizationFeatures } from "@/lib/gramAdminApi";

function Group({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="mt-5 first:mt-0">
      <h5 className="text-muted-foreground mb-1 text-xs font-medium">
        {title}
      </h5>
      {children}
    </section>
  );
}

function Row({
  label,
  value,
}: {
  label: string;
  value: boolean;
}): JSX.Element {
  return (
    <div className="grid grid-cols-[20rem_1fr] items-baseline gap-3 py-1">
      <span className="text-muted-foreground text-sm">{label}</span>
      <span className="text-sm">{value ? "Enabled" : "Disabled"}</span>
    </div>
  );
}

export function FeaturesRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Features key={data.id} org={data} />;
}

export function Features({ org }: { org: AdminOrganization }): JSX.Element {
  const { data, isPending, isError } = useQuery({
    ...organizationFeaturesQuery(org.id),
    enabled: !!org.id,
  });

  if (isPending) {
    return <span className="text-muted-foreground text-sm">Loading...</span>;
  }

  if (isError || !data) {
    return (
      <span className="text-muted-foreground text-sm">
        Unable to load features
      </span>
    );
  }

  return <FeaturesView features={data} />;
}

function FeaturesView({
  features,
}: {
  features: AdminOrganizationFeatures;
}): JSX.Element {
  return (
    <div className="border-border bg-muted/10 rounded-md border p-4">
      <Group title="Feature flags">
        <Row
          label="Consent tool filtering"
          value={features.consent_tool_filtering_enabled}
        />
        <Row
          label="Hooks browser login"
          value={features.hooks_browser_login_enabled}
        />
        <Row label="Hooks fail open" value={features.hooks_fail_open_enabled} />
        <Row label="Platform MCP" value={features.platform_mcp_enabled} />
        <Row
          label="Remote session auto refresh"
          value={features.remote_session_auto_refresh_enabled}
        />
        <Row
          label="Session capture"
          value={features.session_capture_enabled}
        />
        <Row
          label="Skill capture metadata only"
          value={features.skill_capture_metadata_only}
        />
        <Row label="Skills" value={features.skills_enabled} />
      </Group>
    </div>
  );
}
