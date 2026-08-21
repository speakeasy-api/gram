import { PlatformAdminOnlyPanel } from "@/components/platform-admin-only-panel";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin, useOrganization } from "@/contexts/Auth";
import { formatTunneledMcpDisplay } from "@/lib/sources";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { useTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import { useId } from "react";

const DIRECT_ROUTE = "direct-cloud-egress";

export function IssuerTunnelSelector({
  projectId,
  value,
  onChange,
}: {
  projectId: string;
  value: string;
  onChange: (value: string) => void;
}): JSX.Element | null {
  const isPlatformAdmin = useIsPlatformAdmin();
  const selectId = useId();
  const organization = useOrganization();
  const canConfigure = isPlatformAdmin && projectId !== "";
  const projectsQuery = useListProjects(
    { organizationId: organization.id },
    undefined,
    { enabled: canConfigure },
  );
  const projectSlug = projectsQuery.data?.projects.find(
    (project) => project.id === projectId,
  )?.slug;
  const tunnelsQuery = useTunneledMcpServers(
    { gramProject: projectSlug },
    undefined,
    { enabled: canConfigure && projectSlug !== undefined },
  );

  if (!canConfigure) {
    return null;
  }

  const tunnels = tunnelsQuery.data?.tunneledMcpServers ?? [];
  const loadError = projectsQuery.error ?? tunnelsQuery.error;
  const selectedTunnelIsUnavailable =
    value !== "" && !tunnels.some((tunnel) => tunnel.id === value);

  return (
    <PlatformAdminOnlyPanel>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={selectId}>OAuth network route</Label>
        <Select
          value={value || DIRECT_ROUTE}
          onValueChange={(next) => onChange(next === DIRECT_ROUTE ? "" : next)}
          disabled={!projectSlug || tunnelsQuery.isPending}
        >
          <SelectTrigger
            id={selectId}
            className="w-full"
            aria-invalid={loadError ? true : undefined}
            aria-describedby={loadError ? `${selectId}-error` : undefined}
          >
            <SelectValue placeholder="Select a tunnel" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={DIRECT_ROUTE}>Direct cloud egress</SelectItem>
            {selectedTunnelIsUnavailable && (
              <SelectItem value={value} disabled>
                Current tunnel (unavailable)
              </SelectItem>
            )}
            {tunnels.map((tunnel) => (
              <SelectItem
                key={tunnel.id}
                value={tunnel.id}
                description={tunnel.connectionStatus.replaceAll("_", " ")}
              >
                {formatTunneledMcpDisplay(tunnel)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Text small muted>
          Routes metadata discovery, token exchange, refresh, revocation, and
          dynamic client registration through the selected project tunnel.
        </Text>
        {loadError && (
          <Text
            id={`${selectId}-error`}
            role="alert"
            small
            className="text-destructive"
          >
            Failed to load this project's tunnels.
          </Text>
        )}
      </div>
    </PlatformAdminOnlyPanel>
  );
}
