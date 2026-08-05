import { DEMO_ORG_SLUG, PRE_DEMO_ORG_KEY } from "@/lib/demo";
import { useSdkClient } from "@/contexts/Sdk";
import { AuthShell } from "@/pages/login/components/auth-shell";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useEffect, useRef, useState } from "react";

/**
 * Stable link target for entering the shared read-only demo org: switches the
 * current session into it via auth.enterDemo, then reloads into the demo org
 * route. Logged-out visitors bounce through /login and land back here.
 */
export default function ExploreDemo(): JSX.Element {
  const client = useSdkClient();
  const [error, setError] = useState(false);
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void (async () => {
      // Remember where the user came from so Exit demo can return them to
      // the same org. Best-effort: a failed info call just skips the stash.
      try {
        const info = await client.auth.info();
        const activeOrgID = info?.result.activeOrganizationId;
        const activeOrg = info?.result.organizations.find(
          (org) => org.id === activeOrgID,
        );
        if (activeOrg && activeOrg.slug !== DEMO_ORG_SLUG) {
          localStorage.setItem(PRE_DEMO_ORG_KEY, activeOrg.id);
        }
      } catch {
        // ignore — exit falls back to the first non-demo org
      }
      return client.auth.enterDemo();
    })().then(
      // Land directly on the demo org route: a bare "/" gets reconciled
      // against the last-visited org, which would switch the session back.
      () => window.location.replace(`/${DEMO_ORG_SLUG}`),
      (err: unknown) => {
        // No session yet — bounce through login and come back here.
        if (err instanceof GramError && err.statusCode === 401) {
          window.location.replace(
            `/login?redirect=${encodeURIComponent("/explore-demo")}`,
          );
          return;
        }
        setError(true);
      },
    );
  }, [client]);

  return (
    <AuthShell page="Explore demo" showTerms={false}>
      <p className="auth-mono text-[13px] text-[var(--muted)]">
        {error
          ? "The demo organization is not available right now. Please try again later."
          : "Entering the demo organization…"}
      </p>
    </AuthShell>
  );
}
