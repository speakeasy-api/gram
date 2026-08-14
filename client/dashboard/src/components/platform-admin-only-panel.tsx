import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { ShieldAlert } from "lucide-react";
import type { ReactNode } from "react";

// Visual wrapper for Speakeasy-staff-only controls that sit on a customer-
// visible page (project settings, billing meters). Renders nothing for
// everyone else. The platform-admin flag is not an RBAC scope, so this is
// presentation only — each enclosed surface still has to enforce the flag
// server-side.
export function PlatformAdminOnlyPanel({
  children,
}: {
  children: ReactNode;
}): JSX.Element | null {
  const isAdmin = useIsPlatformAdmin();
  if (!isAdmin) {
    return null;
  }

  return (
    <div className="border-destructive-default bg-card border p-4">
      <Stack direction="horizontal" align="center" gap={2} className="mb-3">
        <ShieldAlert className="text-default-destructive h-5 w-5" />
        <Heading variant="h4" className="text-default-destructive">
          Platform Admin Only
        </Heading>
      </Stack>
      {children}
    </div>
  );
}
