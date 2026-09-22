import type { NetworkIngressRolloutStatus } from "@/hooks/useNetworkIngressRollout";

export function shouldShowNetworkAccessPage(
  rolloutStatus: NetworkIngressRolloutStatus,
  canManageIngress: boolean,
): boolean {
  return canManageIngress || rolloutStatus !== "disabled";
}
