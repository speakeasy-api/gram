import { useKillswitchAccess } from "@/hooks/useKillswitchAccess";
import { Outlet } from "react-router";

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
