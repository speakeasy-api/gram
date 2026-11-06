import type { Selector } from "@gram/client/models/components/selector.js";
import type { AnnotationHint, CustomTab, ResourceType } from "./types";
import {
  DISPOSITION_TO_ANNOTATION,
  isProjectFilteredResourceType,
  isProjectScopedResourceType,
  isProjectSelectableResourceType,
} from "./types";

// --- Discriminated union for panel state ---

export type PanelState =
  | AllPanel
  | ProjectsPanel
  | ServersPanel
  | ToolsSelectPanel
  | ToolsAnnotationPanel;

interface AllPanel {
  activePanel: "all";
  label: string;
}

interface ProjectsPanel {
  activePanel: "projects";
  selectedProjectIds: string[];
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

// --- Pure computation ---

/**
 * Whether a selector reaches a whole project: it names the project and no
 * single resource, tool or disposition inside it.
 */
export function isProjectWideSelector(selector: Selector): boolean {
  if (!selector.projectId) return false;
  if (selector.tool || selector.disposition) return false;
  return !selector.resourceId || selector.resourceId === "*";
}

export function computePanelState(
  selectors: Selector[] | null,
  resourceType: ResourceType,
  customTab?: CustomTab,
): PanelState {
  const projectSelectable = isProjectSelectableResourceType(resourceType);
  const projectFiltered = isProjectFilteredResourceType(resourceType);
  const noun = projectSelectable
    ? "project"
    : projectFiltered
      ? resourceType
      : "server";
  const allLabel =
    resourceType === "inference"
      ? "All projects"
      : projectFiltered
        ? "All assistants"
        : isProjectScopedResourceType(resourceType)
          ? "All projects"
          : "All servers";

  // Unrestricted
  if (selectors === null) {
    return {
      activePanel: "all",
      label: allLabel,
    };
  }

  // Empty — user switched mode but hasn't selected anything yet
  if (selectors.length === 0 && resourceType === "inference") {
    return {
      activePanel: "projects",
      selectedProjectIds: [],
      label: "Select...",
    };
  }
  if (selectors.length === 0) {
    return {
      activePanel: "servers",
      selectedServerIds: [],
      label: "Select...",
    };
  }

  // Project-scoped selectors (resource_kind=mcp, resource_id=*, projectId set).
  // A selector naming one resource can carry its project too, as an agent's
  // server grants do; that selector is the resource's, not the project's.
  const projectWide = selectors.filter(isProjectWideSelector);
  if (projectWide.length > 0) {
    const selectedProjectIds = projectWide.map((s) => s.projectId!);
    const count = selectedProjectIds.length;
    return {
      activePanel: "projects",
      selectedProjectIds,
      label:
        count === 0
          ? "Select..."
          : `${count} project${count === 1 ? "" : "s"} selected`,
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
