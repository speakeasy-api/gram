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
 * one without the other: those readers are told where killswitches went rather
 * than forwarded onto a screen that refuses them.
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
 * Where the killswitch roster used to be. There is no longer a list of
 * restrictions to land on, so the reader is sent to the people they are placed
 * on.
 */
export function KillswitchIndexRedirect(): JSX.Element {
  const identitiesHref = useIdentitiesHref();
  if (!identitiesHref) return <KillswitchesMovedNotice />;
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

  const userId = detailQuery.data?.userId;
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
