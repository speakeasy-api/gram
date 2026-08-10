import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import type { ReactNode } from "react";

// Chrome-level gate for the Platform Admin pages. Mirrors the visibility rule
// of the old floating Developer Toolkit: always available in local dev (so
// non-admins can reach the platform-admin impersonation toggle), platform
// admins only everywhere else. Presentation only — every endpoint these pages
// call enforces the platform-admin flag server-side.
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
