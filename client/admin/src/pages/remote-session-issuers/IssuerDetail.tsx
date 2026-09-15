import type { JSX } from "react";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { adminGetGlobalIssuerQuery } from "@/lib/gramAdminClient";
import { IssuerEditor } from "./IssuerEditor";
import { IssuerLogo } from "./IssuerLogo";
import { IssuerConfiguration } from "./IssuerConfiguration";
import { IssuerActions } from "./IssuerActions";
import { ConvergenceSummary } from "./ConvergenceSummary";
import { Convergence } from "./Convergence";
export function IssuerDetail(): JSX.Element | null {
  const { issuerId } = useParams({ from: "/remote-session-issuers/$issuerId" });
  const query = useQuery(adminGetGlobalIssuerQuery({ id: issuerId }));
  return (
    <div className="grid gap-6">
      {query.isPending && <p role="status">Loading issuer…</p>}
      {query.error && (
        <p role="alert">
          {query.error.message}
          <Button variant="ghost" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </p>
      )}
      {query.data && (
        <>
          <header className="flex items-center gap-3">
            {query.data.issuer.logoAssetId && (
              <IssuerLogo id={query.data.issuer.logoAssetId} />
            )}
            <div>
              <h1 className="text-xl font-semibold">
                {query.data.issuer.name || query.data.issuer.issuer}
              </h1>
              <p className="text-muted-foreground text-sm break-all">
                {query.data.issuer.issuer}
              </p>
            </div>
          </header>
          <nav aria-label="Issuer views" className="flex gap-2 border-b pb-3">
            {(
              [
                { to: "/remote-session-issuers/$issuerId", label: "Overview" },
                {
                  to: "/remote-session-issuers/$issuerId/settings",
                  label: "Settings",
                },
                {
                  to: "/remote-session-issuers/$issuerId/convergence",
                  label: "Convergence",
                },
              ] as const
            ).map((tab) => (
              <Button key={tab.label} variant="ghost" size="sm" asChild>
                <Link
                  to={tab.to}
                  params={{ issuerId }}
                  activeOptions={{ exact: true }}
                  activeProps={{
                    className: "bg-muted",
                    "aria-current": "page",
                  }}
                >
                  {tab.label}
                </Link>
              </Button>
            ))}
          </nav>
          <Outlet />
        </>
      )}
    </div>
  );
}
export function IssuerOverview(): JSX.Element | null {
  const { issuerId } = useParams({ from: "/remote-session-issuers/$issuerId" });
  const { data } = useQuery(adminGetGlobalIssuerQuery({ id: issuerId }));
  const navigate = useNavigate();
  return data ? (
    <div className="grid gap-6">
      <dl className="grid gap-3 sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground text-sm">Scope</dt>
          <dd>Platform</dd>
        </div>
        <div>
          <dt className="text-muted-foreground text-sm">
            Platform / tenant clients
          </dt>
          <dd>
            {data.globalClientCount} / {data.tenantClientCount}
          </dd>
        </div>
      </dl>
      <IssuerConfiguration issuer={data.issuer} />
      <ConvergenceSummary issuerId={issuerId} />
      <IssuerActions
        record={data}
        onDeleted={async () => {
          await navigate({ to: "/remote-session-issuers" });
        }}
      />
    </div>
  ) : null;
}
export function IssuerSettings(): JSX.Element | null {
  const { issuerId } = useParams({ from: "/remote-session-issuers/$issuerId" });
  const { data } = useQuery(adminGetGlobalIssuerQuery({ id: issuerId }));
  const [revision, setRevision] = useState(0);
  const [editorPending, setEditorPending] = useState(false);
  const navigate = useNavigate();
  return data ? (
    <div className="grid max-w-2xl gap-6">
      <IssuerEditor
        key={`${issuerId}-${revision}`}
        issuer={data.issuer}
        tenantClientCount={data.tenantClientCount}
        onPendingChange={setEditorPending}
        onDone={() => setRevision((v) => v + 1)}
      />
      <IssuerActions
        record={data}
        showRefresh={false}
        disabled={editorPending}
        onDeleted={async () => {
          await navigate({ to: "/remote-session-issuers" });
        }}
      />
    </div>
  ) : null;
}
export function IssuerConvergence(): JSX.Element | null {
  const { issuerId } = useParams({ from: "/remote-session-issuers/$issuerId" });
  const { data } = useQuery(adminGetGlobalIssuerQuery({ id: issuerId }));
  return (
    <Convergence
      key={issuerId}
      issuerId={issuerId}
      targetName={data?.issuer.name || data?.issuer.issuer || issuerId}
    />
  );
}
