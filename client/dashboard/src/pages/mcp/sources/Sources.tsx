import { ResourceListPage } from "@/components/page-templates";
import {
  SourceCard,
  sourceAssetId,
  useProjectSources,
} from "@/components/sources/source-grid";
import { Button } from "@/components/ui/Button";
import { useRoutes } from "@/routes";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";

export function SourcesRoot(): JSX.Element {
  return <Outlet />;
}

/**
 * What this project has to build servers from.
 *
 * Sources lost their own section when MCP became the inventory, but the CLI
 * still hands people a link after a push and functions still arrive this way,
 * so they keep a place to land — a read-only shelf, with the acting done from
 * the add flow next door.
 */
export default function Sources(): JSX.Element {
  const routes = useRoutes();
  const { sources, isLoading } = useProjectSources();
  const [search, setSearch] = useState("");

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return sources;
    return sources.filter((source) =>
      source.name.toLowerCase().includes(query),
    );
  }, [sources, search]);

  return (
    <ResourceListPage
      scope="mcp:read"
      title="Sources"
      description="The OpenAPI documents and functions this project deploys. Their tools are what an MCP server built from them starts with."
      primaryAction={
        <routes.mcp.add.fromSource.Link>
          <Button variant="primary">
            <Button.Text>Build a server</Button.Text>
          </Button>
        </routes.mcp.add.fromSource.Link>
      }
      search={{
        value: search,
        onChange: setSearch,
        placeholder: "Search sources...",
      }}
      isLoading={isLoading}
      isEmpty={sources.length === 0}
      hideToolbar={sources.length === 0}
      empty={{
        icon: "file-code",
        heading: "No sources yet",
        description:
          "Push an OpenAPI document or a function, from the CLI or the add flow, and it shows up here.",
      }}
    >
      <div className="@2xl/main:grid-cols-2 grid grid-cols-1 gap-4">
        {filtered.map((source) => (
          <SourceCard
            key={source.key}
            source={source}
            // A source has an address of its own here, rather than a sheet:
            // this page is where someone is sent to look one up.
            onInspect={() =>
              routes.mcp.sources.detail.goTo(sourceAssetId(source))
            }
          />
        ))}
      </div>
    </ResourceListPage>
  );
}
