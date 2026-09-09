import { killswitchRecordHref } from "@/components/killswitch/killswitch-routing";
import { useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useKillswitchAccess } from "@/hooks/useKillswitchAccess";
import { useIdentityHrefBuilder } from "@/lib/useIdentityHref";
import { useRoutes } from "@/routes";
import { useKillswitch } from "@gram/client/react-query/killswitch.js";
import { Navigate, Outlet, useParams } from "react-router";

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
      <div className="flex min-h-[420px] items-center justify-center p-8 text-center">
        <div className="max-w-md space-y-2">
          <h1 className="text-xl font-semibold">Killswitch is not available</h1>
          <p className="text-muted-foreground text-sm">
            This customer-admin feature is restricted during rollout. Support
            sessions cannot use it.
          </p>
        </div>
      </div>
    );
  }
  return <Outlet />;
}

/**
 * The directory these addresses resolve into.
 *
 * Killswitches are managed on the identity of the person they restrict, so the
 * project comes from the same slug org-scoped pages already send on their
 * requests — this route carries none in its path.
 */
function useIdentitiesHref(): string {
  const projectSlug = useProjectSlugForRequests();
  return useRoutes({ projectSlug }).identities.href();
}

/**
 * Where the killswitch roster used to be. There is no longer a list of
 * restrictions to land on, so the reader is sent to the people they are placed
 * on.
 */
export function KillswitchIndexRedirect(): JSX.Element {
  return <Navigate to={useIdentitiesHref()} replace />;
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
  // A record we cannot read, or one whose subject this reader cannot open,
  // still has to land somewhere it can act: the directory, rather than a page
  // that no longer exists.
  return (
    <Navigate
      replace
      to={
        accessHref
          ? killswitchRecordHref(accessHref, killswitchId)
          : identitiesHref
      }
    />
  );
}
