import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import {
  AgentRestrictions,
  AgentRestrictionRecord,
} from "@/pages/fleet/AgentRestrictions";
import { Link, useNavigate } from "react-router";
import { killswitchRecordHref } from "@/components/killswitch/killswitch-routing";
import { useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useKillswitchAccess } from "@/hooks/useKillswitchAccess";
import { useRBAC } from "@/hooks/useRBAC";
import { withIdentityWindow } from "@/lib/identity-urn";
import { useIdentityHrefBuilder } from "@/lib/useIdentityHref";
import { useRoutes } from "@/routes";
import { useKillswitch } from "@gram/client/react-query/killswitch.js";
import type { ReactNode } from "react";
import { Navigate, Outlet, useLocation, useParams } from "react-router";

/** A standing message on a route that no longer renders a page of its own. */
function KillswitchNotice({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex min-h-[420px] items-center justify-center p-8 text-center">
      <div className="max-w-md space-y-2">
        <h1 className="text-xl font-semibold">{title}</h1>
        <p className="text-muted-foreground text-sm">{children}</p>
      </div>
    </div>
  );
}

export function KillswitchesRoot(): JSX.Element {
  const access = useKillswitchAccess();
  if (access.isLoading) {
    return (
      <div className="p-8 text-sm text-muted-foreground">
        Checking Killswitch access…
      </div>
    );
  }
  if (!access.canAccess) {
    return (
      <KillswitchNotice title="Killswitch is not available">
        This customer-admin feature is restricted during rollout. Support
        sessions cannot use it.
      </KillswitchNotice>
    );
  }
  return <Outlet />;
}

/**
 * The directory these addresses resolve into, or nothing when the reader
 * cannot open it.
 *
 * Killswitches are managed on the identity of the person they restrict, so the
 * project comes from the same slug org-scoped pages already send on their
 * requests — this route carries none in its path. The roster is a project:read
 * surface while Killswitch is gated on org:admin, and a custom role can grant
 * one without the other. Agent restrictions remain recoverable at this route.
 */
function useIdentitiesHref(): string | null {
  const projectSlug = useProjectSlugForRequests();
  const href = useRoutes({ projectSlug }).identities.href();
  const { search } = useLocation();
  const { hasAnyScope, isLoading } = useRBAC();
  if (!isLoading && !hasAnyScope(["project:read"])) return null;
  return withIdentityWindow(href, search);
}

function KillswitchesMovedNotice(): JSX.Element {
  return (
    <KillswitchNotice title="Killswitches moved">
      A killswitch is now managed on the <strong>Access</strong> tab of the
      person it restricts. Opening the identity directory needs the project:read
      scope, which this account does not have.
    </KillswitchNotice>
  );
}

/**
 * Preserve the person-directory entry point, with an agent recovery list for
 * administrators who cannot open project-scoped Fleet.
 */
export function KillswitchIndexRedirect(): JSX.Element {
  const identitiesHref = useIdentitiesHref();
  const fleetFlag = useFeatureFlag(FEATURE_FLAGS.fleet);
  if (fleetFlag.status === "loading")
    return <div className="p-8 text-sm">Loading restriction navigation…</div>;
  if (!identitiesHref || fleetFlag.status !== "enabled")
    return (
      <div className="p-4 sm:p-8">
        {identitiesHref && (
          <Link
            className="text-sm underline underline-offset-4"
            to={identitiesHref}
          >
            Manage people’s restrictions in Identities
          </Link>
        )}
        <AgentRestrictions agents={[]} inventoryAvailable={false} />
      </div>
    );
  return <Navigate to={identitiesHref} replace />;
}

/**
 * Where one killswitch used to have a page of its own.
 *
 * The record now lives on the access tab of its subject, which the address
 * does not name — so the killswitch is read for the person it restricts and
 * the reader is forwarded onto that person's page with this record open. An
 * audit-log entry linking here still opens the exact record it refers to.
 */
export function KillswitchRecordRedirect(): JSX.Element {
  const { killswitchId = "" } = useParams();
  const navigate = useNavigate();
  const { hasScope } = useRBAC();
  const projectSlug = useProjectSlugForRequests();
  const fleetHref = useRoutes({ projectSlug }).fleet.href();
  const fleetFlag = useFeatureFlag(FEATURE_FLAGS.fleet);
  const session = useSession();
  const identityAccessHref = useIdentityHrefBuilder("access");
  const identitiesHref = useIdentitiesHref();
  const detailQuery = useKillswitch(
    { sessionHeaderGramSession: session.session },
    { id: killswitchId, gramSession: session.session },
    { throwOnError: false, enabled: killswitchId !== "" },
  );

  if (detailQuery.isLoading) {
    return (
      <div className="p-8 text-sm text-muted-foreground">
        Opening Killswitch…
      </div>
    );
  }

  if (detailQuery.data?.principalKind === "agent") {
    return (
      <div className="p-4 sm:p-8">
        <AgentRestrictionRecord
          id={killswitchId}
          agents={[]}
          inventoryAvailable={false}
          onSelect={(id) => {
            void navigate(`../${id}`);
          }}
          onClose={() => {
            void navigate(
              hasScope("project:read") && fleetFlag.status === "enabled"
                ? `${fleetHref}?tab=restrictions`
                : "..",
              { relative: "path" },
            );
          }}
        />
      </div>
    );
  }
  if (detailQuery.data && detailQuery.data.principalKind !== "user")
    return (
      <KillswitchNotice title="Unsupported restriction target">
        This restriction cannot be managed here.
      </KillswitchNotice>
    );
  const userId =
    detailQuery.data?.principalKind === "user"
      ? detailQuery.data.userId
      : undefined;
  const accessHref = userId ? identityAccessHref({ userId }) : null;
  if (accessHref) {
    return (
      <Navigate replace to={killswitchRecordHref(accessHref, killswitchId)} />
    );
  }
  // A record we cannot read, or one whose subject this reader cannot open,
  // still has to land somewhere it can act: the directory, rather than a page
  // that no longer exists.
  if (!identitiesHref) return <KillswitchesMovedNotice />;
  return <Navigate replace to={identitiesHref} />;
}
