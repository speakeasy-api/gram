import { normalizeRemoteUrl } from "@/pages/catalog/remotes";
import { BookOpen } from "lucide-react";
import { useListMCPCatalog } from "@/pages/catalog/hooks";
import { CatalogLogo } from "@/pages/catalog/CatalogLogo";

/** Resolve from the panel's serializable identity, so open guides retain live branding. */
export function SetupGuideIcon({
  registrySpecifier,
  serverUrl,
  iconUrl,
}: {
  registrySpecifier?: string;
  serverUrl?: string;
  iconUrl?: string;
}): JSX.Element {
  const { data } = useListMCPCatalog(undefined, undefined, {
    enabled: !!(registrySpecifier || serverUrl),
    throwOnError: false,
  });
  const server = data?.servers.find((s) =>
    registrySpecifier
      ? s.registrySpecifier === registrySpecifier
      : s.remotes?.some(
          (remote) =>
            normalizeRemoteUrl(remote.url) ===
            normalizeRemoteUrl(serverUrl ?? ""),
        ),
  );
  return (
    <CatalogLogo
      server={{
        registrySpecifier: registrySpecifier ?? "",
        ...server,
        iconUrl: server?.iconUrl || iconUrl,
      }}
      className="size-5 object-contain"
      fallback={<BookOpen className="text-muted-foreground size-4" />}
    />
  );
}
