import {
  useCatalogInstallations,
  useListMCPCatalog,
} from "@/pages/catalog/hooks";
import type { CatalogServer } from "@/pages/catalog/hooks";
import { useRBAC } from "@/hooks/useRBAC";
import type { ActionHandlers, Query, ResourceData } from "../kit/types";
import type { CatalogRecord, CatalogRow, CatalogSignIn } from "./model";

function signIn(server: CatalogServer): CatalogSignIn {
  switch (server.authSetup) {
    case "dcr":
    case "cimd":
      return "automatic";
    case "manual":
      return "manual";
    case "none":
      return "none";
    case "unknown":
    case undefined:
      return server.supportsDcr ? "automatic" : "unknown";
    default:
      return "unknown";
  }
}

function toRecord(server: CatalogServer, added: boolean): CatalogRecord {
  const name = server.title ?? server.registrySpecifier;
  return {
    id: server.registrySpecifier,
    name,
    description: server.description,
    signIn: signIn(server),
    toolCount: server.toolCount,
    added,
    mark: { label: name, iconUrl: server.iconUrl },
    version: server.version,
    url: server.remotes?.[0]?.url,
    repository: server.repository?.url,
    readOnly: server.isReadOnly ?? false,
  };
}

function useCatalog(): Query<CatalogRecord[]> {
  const query = useListMCPCatalog(undefined, undefined, {
    throwOnError: false,
  });
  const { installState } = useCatalogInstallations();
  if (query.isPending) return { status: "loading" };
  if (query.isError) {
    return {
      status: "error",
      message:
        query.error instanceof Error
          ? query.error.message
          : "Something went wrong.",
      retry: () => void query.refetch(),
    };
  }
  return {
    status: "ready",
    data: (query.data.servers as CatalogServer[]).map((server) =>
      toRecord(server, installState(server) === "added"),
    ),
  };
}

function useRecord(id: string): Query<CatalogRecord> {
  const all = useCatalog();
  if (all.status !== "ready") return all;
  const rec = all.data.find((r) => r.id === id);
  return rec
    ? { status: "ready", data: rec }
    : {
        status: "error",
        message: "It isn't in the reviewed catalog.",
        retry: () => {},
      };
}

function useRecordActions(rec: CatalogRecord | undefined): ActionHandlers {
  const { hasScope } = useRBAC();
  if (!rec) return {};
  // Adding runs the catalog install flow, which the assistant drives.
  return {
    ask: { run: () => {}, pending: false },
    add: hasScope("mcp:write") ? { run: () => {}, pending: false } : undefined,
  };
}

export const catalogSdkData: ResourceData<CatalogRow, CatalogRecord> = {
  useList: useCatalog,
  useRecord,
  useListActions: () => ({}),
  useRecordActions,
};
