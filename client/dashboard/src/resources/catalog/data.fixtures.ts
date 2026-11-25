import { fixtureData, type FixtureState } from "../kit/fixtures";
import type { ResourceData } from "../kit/types";
import { catalogDefinition } from "./definition";
import type { CatalogRecord, CatalogRow } from "./model";

function entry(
  partial: Partial<CatalogRecord> & Pick<CatalogRecord, "id" | "name">,
): CatalogRecord {
  return {
    description: "",
    signIn: "automatic",
    added: false,
    mark: { label: partial.name },
    version: "1.0.0",
    readOnly: false,
    ...partial,
  };
}

const CATALOG_RECORDS: CatalogRecord[] = [
  entry({
    id: "app.linear/mcp",
    name: "Linear",
    description: "Issues, projects and cycles",
    toolCount: 24,
    url: "https://mcp.linear.app/mcp",
    added: true,
  }),
  entry({
    id: "com.atlassian/mcp",
    name: "Atlassian",
    description: "Jira and Confluence Cloud",
    url: "https://mcp.atlassian.com/v1/mcp",
  }),
  entry({
    id: "com.hubspot/mcp",
    name: "HubSpot",
    description: "CRM records",
    signIn: "unknown",
  }),
  entry({
    id: "com.example.internal/very-long-catalog-reference-for-testing/mcp",
    name: "Internal knowledge base search with a deliberately long name",
    description:
      "Searches every published article, runbook and decision record across the company's internal wikis",
    signIn: "manual",
    readOnly: true,
  }),
];

export function catalogFixtureData({
  state = "ready",
  readOnly = false,
}: { state?: FixtureState; readOnly?: boolean } = {}): ResourceData<
  CatalogRow,
  CatalogRecord
> {
  return fixtureData({
    definition: catalogDefinition,
    rows: CATALOG_RECORDS,
    records: Object.fromEntries(CATALOG_RECORDS.map((r) => [r.id, r])),
    state,
    readOnly,
  });
}
