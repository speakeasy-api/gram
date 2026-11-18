import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useSyncExternalStore } from "react";

function subscribe(onChange: () => void) {
  window.addEventListener("rbac-override-change", onChange);
  window.addEventListener("storage", onChange);
  return () => {
    window.removeEventListener("rbac-override-change", onChange);
    window.removeEventListener("storage", onChange);
  };
}

/** The effective request override, for caches that contain permission decisions. */
export function useRBACScopeOverride(): string | null {
  const isPlatformAdmin = useIsPlatformAdmin();
  return useSyncExternalStore(
    subscribe,
    () => getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin),
    () => null,
  );
}
