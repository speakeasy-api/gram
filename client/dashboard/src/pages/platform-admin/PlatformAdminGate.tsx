import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import type { ReactNode } from "react";

// Chrome-level gate for the Platform Admin pages. Mirrors the visibility rule
// of the old floating Developer Toolkit: always available in local dev (so
// non-admins can reach the platform-admin impersonation toggle), platform
// admins only everywhere else. Presentation only — authorization is enforced
// server-side by every endpoint these pages call: staff-managed entitlement
// toggles require the platform-admin flag, org-self-serve features surfaced
// here (e.g. Platform MCP access) require org:admin.
export function PlatformAdminGate({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  const isPlatformAdmin = useIsPlatformAdmin();

  if (!(import.meta.env.DEV || isPlatformAdmin)) {
    return (
      <Text muted className="py-8 text-center">
        This page is available to platform admins only.
      </Text>
    );
  }

  return <>{children}</>;
}
