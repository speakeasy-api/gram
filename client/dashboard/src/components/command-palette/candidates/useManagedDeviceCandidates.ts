import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { isProviderIdVisible } from "@/pages/org/device-integrations/provider-role";
import {
  LINKED_DEVICE_PARAM,
  type LinkedDeviceState,
} from "@/pages/org/device-integrations/coverage-states";
import { useOrgRoutes } from "@/routes";
import {
  buildManagedDevicesInfiniteQuery,
  type ManagedDevicesPageParams,
} from "@gram/client/react-query/managedDevices.js";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/**
 * MDM-synced devices by hostname or serial, opening the device's panel on its
 * integration page. Hostnames often carry a person's name and every device
 * names its assigned user, so these stay fuzzy-only.
 */
export function useManagedDeviceCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();

  const devicesQuery = buildManagedDevicesInfiniteQuery(sdk, { limit: 200 });
  const {
    data,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
  } = useInfiniteQuery({
    ...devicesQuery,
    queryKey: [...devicesQuery.queryKey, organization.id],
    initialPageParam: undefined as ManagedDevicesPageParams,
    getNextPageParam: (page) => page["~next"],
    enabled,
    retry: false,
    throwOnError: false,
  });

  // A device on a later page must still be findable by name.
  useEffect(() => {
    if (!enabled || !hasNextPage || isFetchingNextPage || isFetchNextPageError)
      return;
    void fetchNextPage().catch(() => undefined);
  }, [
    enabled,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);

  return useMemo(() => {
    if (!enabled) return [];
    return (data?.pages ?? [])
      .flatMap((page) => page.result.devices)
      .filter((device) => isProviderIdVisible(device.provider))
      .map((device): LauncherCandidate => {
        const href = orgRoutes.deviceAgent.mdmDetail.href(device.provider);
        return {
          id: `managed-device:${device.id}`,
          kind: "page",
          title: device.hostname || device.serialNumber || "Unnamed device",
          detail: ["Device", device.serialNumber, device.userEmail]
            .filter(Boolean)
            .join(" · "),
          keywords: [
            "device",
            device.provider,
            device.serialNumber ?? "",
            device.externalId,
            device.userEmail ?? "",
          ].filter(Boolean),
          verbs: ["open"],
          fuzzyOnly: true,
          icon: "laptop",
          group: "Devices",
          run: () => {
            const state: LinkedDeviceState = { device };
            void navigate(
              `${href}?${LINKED_DEVICE_PARAM}=${encodeURIComponent(device.id)}`,
              { state },
            );
          },
        };
      });
  }, [data, navigate, orgRoutes, enabled]);
}
