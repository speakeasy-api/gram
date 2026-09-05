import { FormPage } from "@/components/page-templates";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { SourceCard } from "@/components/sources/source-grid";
import {
  sourceAssetId,
  useProjectSources,
} from "@/components/sources/source-list";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useSidePanel } from "@/components/side-panel/side-panel-context";
import { useSdkClient } from "@/contexts/Sdk";
import { useListTools } from "@/hooks/toolTypes";
import { useRoutes } from "@/routes";
import { Loader2 } from "lucide-react";
import { SearchBar } from "@/components/ui/SearchBar";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";

const VISIBLE_SOURCE_LIMIT = 10;

/**
 * Builds an MCP server from a source this project already has.
 *
 * A source-backed server is a toolset, so this creates one and seeds it with
 * every tool the chosen source produced — the step that was missing when the
 * only route to a toolset was an empty one you then had to fill by hand.
 */
export default function CreateFromSource(): JSX.Element {
  const routes = useRoutes();
  const client = useSdkClient();
  const { openPanel } = useSidePanel();
  const { sources, isLoading: isLoadingDeployment } = useProjectSources();
  const { data: toolsResult, isLoading: isLoadingTools } = useListTools();

  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [searchParams] = useSearchParams();
  const [search, setSearch] = useState("");
  const [showAll, setShowAll] = useState(false);
  const [name, setName] = useState("");
  // Until the name is the user's own, it follows the selection: picking a
  // different source should not leave the previous source's name behind.
  const [isNameOwned, setIsNameOwned] = useState(false);
  const [isCreating, setIsCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return sources;
    return sources.filter((source) =>
      source.name.toLowerCase().includes(query),
    );
  }, [sources, search]);

  // A project can carry a lot of sources; show a screenful and let the rest be
  // asked for, rather than a wall of cards above the name field.
  const visible = showAll ? filtered : filtered.slice(0, VISIBLE_SOURCE_LIMIT);
  const hiddenCount = filtered.length - visible.length;

  // Arriving from a source's own page, that source is already the choice: the
  // flow opens on it rather than asking again. Runs once the sources land, and
  // only while nothing is picked, so it never fights a later click.
  const requestedKey = searchParams.get("source");
  useEffect(() => {
    if (selectedKey != null || !requestedKey) return;
    const requested = sources.find((source) => source.key === requestedKey);
    if (!requested) return;
    setSelectedKey(requested.key);
    setName((current) => (current.trim() === "" ? requested.name : current));
    // The arrival is only convincing if the chosen card is visible, and it may
    // sit past the ten shown by default.
    setShowAll(true);
  }, [requestedKey, selectedKey, sources]);

  const selected = sources.find((source) => source.key === selectedKey);

  // The tools this source produced, which become the new server's tools.
  const toolUrns = useMemo(() => {
    if (!selected) return [];
    return (toolsResult?.tools ?? [])
      .filter((tool) => {
        if (selected.kind === "openapi") {
          return (
            tool.type === "http" &&
            tool.openapiv3DocumentId === selected.documentId
          );
        }
        return (
          tool.type === "function" && tool.functionId === selected.functionId
        );
      })
      .map((tool) => tool.toolUrn);
  }, [selected, toolsResult]);

  const isLoading = isLoadingDeployment || isLoadingTools;

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!selected || !name.trim()) return;
    setError(null);
    setIsCreating(true);
    try {
      const toolset = await client.toolsets.create({
        createToolsetRequestBody: { name: name.trim() },
      });
      // Created empty, then filled: the create call takes no tools, so the
      // seeding is a second write. A server with no tools is still a usable
      // landing point if this half fails, so the error is surfaced rather than
      // rolled back.
      if (toolUrns.length > 0) {
        await client.toolsets.updateBySlug({
          slug: toolset.slug,
          updateToolsetRequestBody: { toolUrns },
        });
      }
      toast.success(
        toolUrns.length > 0
          ? `Created "${toolset.name}" with ${toolUrns.length} tool${toolUrns.length === 1 ? "" : "s"}`
          : `Created "${toolset.name}"`,
      );
      routes.mcp.details.tools.goTo(toolset.slug);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to create MCP server",
      );
    } finally {
      setIsCreating(false);
    }
  };

  return (
    <FormPage
      scope="mcp:write"
      width="wide"
      title="New MCP server from a source"
      description="Pick an OpenAPI document or function already in this project. Its tools become the server's tools."
    >
      <form
        onSubmit={(e) => {
          void handleSubmit(e);
        }}
        noValidate
      >
        <Stack gap={8}>
          <Stack gap={4}>
            {isLoading && (
              <Text small muted>
                Loading sources…
              </Text>
            )}
            {!isLoading && sources.length === 0 && (
              <Alert variant="warning" dismissible={false}>
                This project has no OpenAPI documents or functions yet. Add one
                from the Advanced section first.
              </Alert>
            )}
            {sources.length > 0 && (
              <SearchBar
                value={search}
                onChange={setSearch}
                placeholder="Search sources..."
              />
            )}
            {sources.length > 0 && (
              <div className="@2xl/main:grid-cols-2 grid grid-cols-1 gap-4">
                {visible.map((source) => (
                  <SourceCard
                    key={source.key}
                    source={source}
                    selected={source.key === selectedKey}
                    onSelect={() => {
                      setSelectedKey(source.key);
                      // The source name is the obvious default, and most
                      // people keep it.
                      if (!isNameOwned) setName(source.name);
                    }}
                    onInspect={() =>
                      openPanel({
                        kind: "source",
                        title: source.name,
                        subtitle: "Source",
                        props: {
                          sourceKind: source.kind,
                          assetId: sourceAssetId(source),
                        },
                      })
                    }
                  />
                ))}
              </div>
            )}
            {hiddenCount > 0 && (
              <div>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={() => setShowAll(true)}
                >
                  <Button.Text>{`View all ${filtered.length}`}</Button.Text>
                </Button>
              </div>
            )}
            {sources.length > 0 && filtered.length === 0 && (
              <Text small muted>
                No sources match “{search}”.
              </Text>
            )}
          </Stack>

          <div className="border-foreground/10 border-t" />

          <Stack gap={1}>
            <label
              htmlFor="from-source-name"
              className="text-sm leading-none font-medium"
            >
              Server name
            </label>
            <Input
              id="from-source-name"
              placeholder="My MCP server"
              value={name}
              onChange={(value) => {
                setName(value);
                // Clearing the field hands the name back to the selection.
                setIsNameOwned(value.trim() !== "");
              }}
            />
            {selected && (
              <Text muted small>
                {toolUrns.length === 0
                  ? "This source has no tools yet — the server starts empty."
                  : `${toolUrns.length} tool${toolUrns.length === 1 ? "" : "s"} from this source will be added.`}
              </Text>
            )}
          </Stack>

          {error && (
            <Alert variant="error" dismissible={false}>
              {error}
            </Alert>
          )}

          <Stack direction="horizontal" gap={2}>
            <Button
              type="submit"
              variant="primary"
              disabled={!selected || !name.trim() || isCreating}
            >
              {isCreating ? (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>{isCreating ? "Creating" : "Create"}</Button.Text>
            </Button>
            <Button
              type="button"
              variant="secondary"
              disabled={isCreating}
              onClick={() => routes.mcp.add.goTo()}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </form>
    </FormPage>
  );
}
