import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { AuthLayout } from "@/pages/login/components/login-section";
import { JourneyDemo } from "@/pages/login/components/journey-demo";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@speakeasy-api/moonshine";
import { LogOutIcon, AlertCircleIcon, BuildingIcon } from "lucide-react";
import { useState } from "react";

interface SwitchOrgProps {
  gate?: boolean;
}

export default function SwitchOrg({ gate = false }: SwitchOrgProps) {
  const client = useSdkClient();
  const { session } = useSessionData();

  const allOrgs = session?.organizations ?? [];
  const otherOrgs = allOrgs.filter(
    (org) => org.id !== session?.activeOrganizationId,
  );

  const [selectedOrgId, setSelectedOrgId] = useState<string>("");
  const [isSwitching, setIsSwitching] = useState(false);

  const handleSwitch = async () => {
    if (!selectedOrgId || selectedOrgId === session?.activeOrganizationId)
      return;
    setIsSwitching(true);
    try {
      await client.auth.switchScopes({ organizationId: selectedOrgId });
      window.location.replace("/");
    } finally {
      setIsSwitching(false);
    }
  };

  const handleLogout = async () => {
    await client.auth.logout();
    window.location.href = "/login";
  };

  const currentOrgName = session?.organization?.name ?? "This organization";

  return (
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />

      <AuthLayout
        topRight={
          gate ? (
            <button
              onClick={handleLogout}
              className="flex items-center gap-1.5 text-xs text-[#8B8684] transition-colors hover:text-slate-600"
            >
              <LogOutIcon className="h-3.5 w-3.5" />
              Log out
            </button>
          ) : undefined
        }
      >
        <div className="flex flex-col items-center gap-3 text-center">
          <div
            className={`flex h-12 w-12 items-center justify-center rounded-full ${gate ? "bg-amber-50" : "bg-blue-50"}`}
          >
            {gate ? (
              <AlertCircleIcon className="h-6 w-6 text-amber-500" />
            ) : (
              <BuildingIcon className="h-6 w-6 text-blue-500" />
            )}
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-gray-900">
            {gate ? `No access for ${currentOrgName}` : "Switch organization"}
          </h1>
          <p className="text-sm leading-relaxed text-[#8B8684]">
            {gate
              ? "This organization doesn't have access to the MCP platform. Switch to another organization to continue."
              : "Select which organization you'd like to work in."}
          </p>
        </div>

        <div className="flex w-full flex-col gap-3">
          <Select value={selectedOrgId} onValueChange={setSelectedOrgId}>
            <SelectTrigger className="w-full">
              <SelectValue placeholder="Select an organization" />
            </SelectTrigger>
            <SelectContent>
              {allOrgs.map((org) => {
                const isCurrent = org.id === session?.activeOrganizationId;
                return (
                  <SelectItem key={org.id} value={org.id} disabled={isCurrent}>
                    <span className="flex items-center gap-2">
                      {org.name || org.slug}
                      {isCurrent && (
                        <span className="rounded-sm bg-emerald-50 px-1.5 py-0.5 text-[10px] font-medium text-emerald-600">
                          current
                        </span>
                      )}
                    </span>
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>

          <Button
            variant="brand"
            className="w-full"
            onClick={handleSwitch}
            disabled={
              !selectedOrgId ||
              selectedOrgId === session?.activeOrganizationId ||
              isSwitching
            }
          >
            {isSwitching ? "Switching…" : "Switch organization"}
          </Button>
        </div>
      </AuthLayout>
    </main>
  );
}
