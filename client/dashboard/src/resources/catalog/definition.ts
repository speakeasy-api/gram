import { defineResource, type Status } from "../kit/types";
import { SIGN_IN_LABEL, type CatalogRecord, type CatalogRow } from "./model";

function signInStatus(row: CatalogRow): Status {
  switch (row.signIn) {
    case "automatic":
      return { label: "Automatic", tone: "success" };
    case "none":
      return { label: "None needed", tone: "neutral" };
    case "manual":
      return { label: "Manual setup", tone: "warning" };
    case "unknown":
      return { label: "Unknown", tone: "neutral" };
  }
}

function catalogRef(input: unknown): string | undefined {
  if (input == null || typeof input !== "object") return undefined;
  const ref = (input as Record<string, unknown>)["catalog_ref"];
  return typeof ref === "string" && ref ? ref : undefined;
}

export const catalogDefinition = defineResource<CatalogRow, CatalogRecord>({
  id: "catalog",
  noun: { one: "catalog server", many: "Catalog" },
  area: "Connect",

  list: {
    title: "Catalog",
    description:
      "Reviewed MCP servers you can add to this project in a few clicks.",
    rowId: (row) => row.id,
    layout: "cards",
    card: {
      badge: (row) =>
        row.added ? { text: "Added", tone: "success" } : undefined,
      facts: (row) => [
        { kind: "status", status: signInStatus(row) },
        ...(row.toolCount != null
          ? [
              {
                kind: "number" as const,
                value: row.toolCount,
                unit: { one: "tool", many: "tools" },
              },
            ]
          : []),
      ],
    },
    searchText: (row) => `${row.name} ${row.description} ${row.id}`,
    columns: [
      {
        id: "name",
        header: "Server",
        kind: "identity",
        value: (row) => ({
          mark: row.mark,
          title: row.name,
          subtitle: row.description,
        }),
      },
      {
        id: "signIn",
        header: "Sign-in",
        kind: "value",
        value: (row) => ({ kind: "status", status: signInStatus(row) }),
      },
      {
        id: "added",
        header: "In project",
        kind: "value",
        value: (row) =>
          row.added
            ? { kind: "tag", text: "Added", tone: "success" }
            : { kind: "empty", text: "—" },
      },
    ],
    filters: [
      {
        id: "added",
        label: "In project",
        options: [
          { value: "new", label: "Not added" },
          { value: "added", label: "Added" },
        ],
        matches: (row, value) =>
          value === "added" ? row.added === true : row.added === false,
      },
    ],
    empty: {
      icon: "library",
      heading: "The catalog is empty",
      description:
        "No reviewed servers are available to this organization yet. You can still connect any server by its URL.",
    },
  },

  record: {
    header: (rec) => ({
      kicker: "Catalog",
      identity: { mark: rec.mark, title: rec.name },
      description: rec.description,
      status: rec.added
        ? { label: "Added to this project", tone: "success" }
        : undefined,
    }),
    primaryAction: (rec) => (rec.added === false ? "add" : undefined),
    secondaryActions: ["ask"],
    sections: [
      {
        id: "facts",
        kind: "facts",
        facts: [
          {
            label: "Sign-in",
            value: (rec) => ({ kind: "text", text: SIGN_IN_LABEL[rec.signIn] }),
          },
          {
            label: "Tools",
            value: (rec) =>
              rec.toolCount == null
                ? { kind: "empty", text: "Listed after adding" }
                : { kind: "number", value: rec.toolCount },
          },
          {
            label: "Access",
            value: (rec) =>
              rec.readOnly === undefined
                ? { kind: "empty", text: "Not stated" }
                : {
                    kind: "text",
                    text: rec.readOnly ? "Read-only" : "Read and write",
                  },
          },
          {
            label: "Version",
            value: (rec) => ({ kind: "text", text: rec.version, mono: true }),
          },
        ],
      },
      {
        id: "source",
        kind: "list",
        title: "Where it comes from",
        empty: "No public source listed.",
        items: (rec) => [
          ...(rec.url
            ? [
                {
                  id: "url",
                  title: "Endpoint",
                  description: rec.url,
                  mono: true,
                },
              ]
            : []),
          ...(rec.repository
            ? [
                {
                  id: "repo",
                  title: "Repository",
                  description: rec.repository,
                  mono: true,
                },
              ]
            : []),
          {
            id: "ref",
            title: "Catalog reference",
            description: rec.id,
            mono: true,
          },
        ],
      },
    ],
  },

  actions: [
    {
      id: "add",
      label: "Add to project",
      kind: "chat",
      scope: "mcp:write",
      prompt: "Add {title} from the catalog to this project.",
      sdk: ["useRemoteMcpInstallWorkflow"],
    },
    {
      id: "ask",
      label: "Ask about this",
      kind: "chat",
      prompt:
        "Tell me what {title} from the catalog can do, and what it needs to set up.",
      sdk: [],
    },
  ],

  chat: {
    tools: {
      search_mcp_catalog: { opens: "list" },
      inspect_mcp_candidate: {
        opens: "record",
        id: (input) => catalogRef(input),
      },
    },
    keywords: [
      "catalog",
      "catalogue",
      "registry",
      "marketplace",
      "install",
      "add",
    ],
    suggestions: ["Show me the MCP catalog"],
  },
});
