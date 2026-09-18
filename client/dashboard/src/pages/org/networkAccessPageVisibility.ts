import type { NetworkIngressRolloutStatus } from "@/hooks/useNetworkIngressRollout";

export function shouldShowNetworkAccessPage(
  rolloutStatus: NetworkIngressRolloutStatus,
  canManageIngress: boolean,
  ingressMayExist: boolean,
): boolean {
  return rolloutStatus !== "disabled" || (canManageIngress && ingressMayExist);
}
