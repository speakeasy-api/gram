import type {
  AnnotationHint,
  CustomTab,
  ResourceType,
  Selector,
} from "./types";
import { DISPOSITION_TO_ANNOTATION } from "./types";

// --- Collection group shape (minimal interface for computation) ---

export interface CollectionGroup {
  id: string;
  name: string;
  servers: { id: string; tools: { name: string }[] }[];
}

// --- Discriminated union for panel state ---

export type PanelState =
  | AllPanel
  | ServersPanel
  | ToolsSelectPanel
  | ToolsAnnotationPanel
  | CollectionPanel;

interface AllPanel {
  activePanel: "all";
  label: string;
}

interface ServersPanel {
  activePanel: "servers";
  selectedServerIds: string[];
  label: string;
}

interface ToolsSelectPanel {
  activePanel: "tools";
  tab: "select";
  selectedTools: { serverId: string; tool: string }[];
  label: string;
}

interface ToolsAnnotationPanel {
  activePanel: "tools";
  tab: "auto-groups";
  annotations: AnnotationHint[];
  label: string;
}

interface CollectionPanel {
  activePanel: "collection";
  selectedCollectionCount: number;
  label: string;
}

// --- Pure computation ---

export function computePanelState(
  selectors: Selector[] | null,
  collectionGroups: CollectionGroup[],
  resourceType: ResourceType,
  customTab?: CustomTab,
): PanelState {
  const noun = resourceType === "project" ? "project" : "server";

  // Unrestricted
  if (selectors === null) {
    return {
      activePanel: "all",
      label: resourceType === "project" ? "All projects" : "All servers",
    };
  }

  // Empty — user switched mode but hasn't selected anything yet
  if (selectors.length === 0) {
    return {
      activePanel: "servers",
      selectedServerIds: [],
      label: "Select...",
    };
  }

  // Disposition-based (annotation auto-groups)
  const hasDisposition = selectors.some((s) => s.disposition);
  if (hasDisposition) {
    const annotations = selectors
      .map((s) =>
        s.disposition ? DISPOSITION_TO_ANNOTATION[s.disposition] : undefined,
      )
      .filter((a): a is AnnotationHint => !!a);
    const count = annotations.length;
    return {
      activePanel: "tools",
      tab: "auto-groups",
      annotations,
      label:
        count === 0
          ? "Select..."
          : `${count} rule${count === 1 ? "" : "s"} selected`,
    };
  }

  // Tool-level selectors
  const hasTools = selectors.some((s) => s.tool);
  if (hasTools) {
    // Check if selectors match collection groups
    const collectionCount = getSelectedCollectionCount(
      selectors,
      collectionGroups,
    );
    if (collectionCount > 0) {
      return {
        activePanel: "collection",
        selectedCollectionCount: collectionCount,
        label: `${collectionCount} collection${collectionCount === 1 ? "" : "s"} selected`,
      };
    }

    // Individual tool selection
    const selectedTools = selectors
      .filter((s) => s.tool && s.resourceId)
      .map((s) => ({ serverId: s.resourceId!, tool: s.tool! }));
    const count = selectedTools.length;
    return {
      activePanel: "tools",
      tab: customTab === "auto-groups" ? "auto-groups" : "select",
      annotations: [],
      selectedTools,
      label:
        count === 0
          ? "Select..."
          : `${count} tool${count === 1 ? "" : "s"} selected`,
    } as ToolsSelectPanel;
  }

  // Server-level selectors only
  const selectedServerIds = selectors
    .filter((s) => s.resourceId)
    .map((s) => s.resourceId!);
  const count = selectedServerIds.length;
  return {
    activePanel: "servers",
    selectedServerIds,
    label:
      count === 0
        ? "Select..."
        : `${count} ${noun}${count === 1 ? "" : "s"} selected`,
  };
}

/**
 * Counts how many collection groups are fully selected (all their tools
 * present in selectors).
 */
export function getSelectedCollectionCount(
  selectors: Selector[],
  collectionGroups: CollectionGroup[],
): number {
  return collectionGroups.filter((group) => {
    const allToolSelectors = group.servers.flatMap((s) =>
      s.tools.map((t) => ({ resourceId: s.id, tool: t.name })),
    );
    if (allToolSelectors.length === 0) return false;
    return allToolSelectors.every((ts) =>
      selectors.some(
        (s) => s.resourceId === ts.resourceId && s.tool === ts.tool,
      ),
    );
  }).length;
}
