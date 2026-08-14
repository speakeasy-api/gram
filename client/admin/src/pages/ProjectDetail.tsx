import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams, Link } from "@tanstack/react-router";
import { cn } from "@/lib/utils";
import { projectQuery } from "@/lib/adminQueries";
import { errorMessage } from "@/lib/gramAdminApi";

function fmtDate(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "-" : d.toLocaleString();
}

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-[14rem_1fr] items-baseline gap-3 py-1">
      <span className="text-muted-foreground text-sm">{label}</span>
      <div>{children}</div>
    </div>
  );
}

function CountTile({ label, value }: { label: string; value: number }) {
  return (
    <div className="border-border bg-muted/10 flex flex-col gap-1 rounded-md border p-3">
      <span className="text-muted-foreground text-sm">{label}</span>
      <h4 className="text-[1.438rem] leading-[1.6] font-light">{value}</h4>
    </div>
  );
}

// The id arrives as a prop rather than out of `useParams`, because this page is
// reached by two routes: the global project list and the organization record's
// own project view. Each names the parameter differently.
export function ProjectDetail({ idOrSlug }: { idOrSlug: string }): JSX.Element {
  const { data, isLoading, isError, error } = useQuery({
    ...projectQuery(idOrSlug),
    enabled: !!idOrSlug,
  });

  return (
    <div className="space-y-6">
      <section>
        <div className="mb-4">
          <h4 className="text-[1.438rem] leading-[1.6] font-light">
            {data ? data.name : "Project"}
          </h4>
          <span className="text-muted-foreground text-sm">{idOrSlug}</span>
        </div>

        {isLoading && (
          <span className="text-muted-foreground text-sm">Loading...</span>
        )}
        {isError && (
          <span className="text-muted-foreground text-sm">
            Error: {errorMessage(error)}
          </span>
        )}

        {data && (
          <>
            <div className="border-border bg-muted/10 rounded-md border p-4">
              <Row label="ID">
                <span className="text-sm">{data.id}</span>
              </Row>
              <Row label="Name">
                <span className="text-sm">{data.name}</span>
              </Row>
              <Row label="Slug">
                <span className="text-sm">{data.slug}</span>
              </Row>
              <Row label="Organization ID">
                <Link
                  to="/organizations/$idOrSlug"
                  params={{ idOrSlug: data.organization_id }}
                  className="text-primary hover:underline"
                >
                  {data.organization_id}
                </Link>
              </Row>
              <Row label="Logo asset ID">
                <span
                  className={cn(
                    "text-sm",
                    !data.logo_asset_id && "text-muted-foreground",
                  )}
                >
                  {data.logo_asset_id ?? "-"}
                </span>
              </Row>
              <Row label="Functions runner version">
                <span
                  className={cn(
                    "text-sm",
                    !data.functions_runner_version && "text-muted-foreground",
                  )}
                >
                  {data.functions_runner_version ?? "-"}
                </span>
              </Row>
              <Row label="Created">
                <span className="text-sm">{fmtDate(data.created_at)}</span>
              </Row>
              <Row label="Updated">
                <span className="text-sm">{fmtDate(data.updated_at)}</span>
              </Row>
            </div>

            <div className="mt-4">
              <h4 className="text-[1.438rem] leading-[1.6] font-light">
                Resource counts
              </h4>
              <div className="mt-2 grid grid-cols-2 gap-2 md:grid-cols-3 lg:grid-cols-6">
                <CountTile label="Toolsets" value={data.toolset_count} />
                <CountTile label="Deployments" value={data.deployment_count} />
                <CountTile label="HTTP tools" value={data.http_tool_count} />
                <CountTile
                  label="Environments"
                  value={data.environment_count}
                />
                <CountTile label="API keys" value={data.api_key_count} />
                <CountTile label="Assistants" value={data.assistant_count} />
              </div>
            </div>
          </>
        )}
      </section>
    </div>
  );
}

export function ProjectDetailRoute(): JSX.Element {
  const { idOrSlug } = useParams({ from: "/projects/$idOrSlug" });
  return <ProjectDetail idOrSlug={idOrSlug} />;
}
