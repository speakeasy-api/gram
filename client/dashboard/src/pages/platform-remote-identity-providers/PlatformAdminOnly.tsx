import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import type { ReactNode } from "react";

// PlatformAdminOnly is the chrome-level gate for the platform catalog routes.
// There is no RequireScope equivalent for the platform-admin flag: it is not an
// RBAC scope, so `useRBAC` never grants it and a Speakeasy admin viewing a
// customer organization typically holds no org grants at all. Gating these
// surfaces on org:admin would therefore lock out exactly the people they are
// for, on top of describing the wrong permission.
//
// This is presentation only. Every adminRemoteSessions endpoint enforces the
// flag server-side in requirePlatformAdmin, so a non-admin who reaches the
// route gets nothing from the API either.
export function PlatformAdminOnly({
  children,
  feature = "This page",
}: {
  children: ReactNode;
  // Names the gated surface in the refusal message. Defaults to wording that
  // reads correctly for any caller, so a second platform-admin surface can
  // reuse this without the message naming the wrong page.
  feature?: string;
}): JSX.Element {
  const isPlatformAdmin = useIsPlatformAdmin();

  if (!isPlatformAdmin) {
    return (
      <Text muted className="py-8 text-center">
        {feature} is available to platform admins only.
      </Text>
    );
  }

  return <>{children}</>;
}
