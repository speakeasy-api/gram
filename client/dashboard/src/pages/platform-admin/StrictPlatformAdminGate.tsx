import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";

// Unlike PlatformAdminGate, no local-dev bypass: pages behind this gate manage
// real platform state shared by every organization. Local developers reach
// them through the impersonation toggle on the Overview page.
export function StrictPlatformAdminGate({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  const isPlatformAdmin = useIsPlatformAdmin();

  if (!isPlatformAdmin) {
    return (
      <Text muted className="py-8 text-center">
        This page is available to platform admins only.
      </Text>
    );
  }

  return <>{children}</>;
}
