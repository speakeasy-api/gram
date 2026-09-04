import { FormPage } from "@/components/page-templates";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Card } from "@/components/ui/Card";
import { useIconConfetti } from "@/components/icon-confetti";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useSidePanel } from "@/components/side-panel/side-panel-context";
import { useSdkClient } from "@/contexts/Sdk";
import { useLatestDeployment, useListTools } from "@/hooks/toolTypes";
import { useRoutes } from "@/routes";
import { Check, Code, FileCode, Loader2 } from "lucide-react";
import { SearchBar } from "@/components/ui/SearchBar";
import { useMemo, useState } from "react";
import { toast } from "sonner";

const VISIBLE_SOURCE_LIMIT = 10;

type SourceOption = {
  key: string;
  name: string;
  kind: "openapi" | "function";
  /** Tools are matched to their source by these ids. */
  documentId?: string;
  functionId?: string;
};

function SourceCard({
  source,
  selected,
  onSelect,
  onInspect,
}: {
  source: SourceOption;
  selected: boolean;
  onSelect: () => void;
  onInspect: () => void;
}): JSX.Element {
  const { canvasRef, start, stop } = useIconConfetti();
  const Icon = source.kind === "openapi" ? FileCode : Code;
  return (
    <div onMouseEnter={start} onMouseLeave={stop} className="h-full">
      <Card.Entity
        onClick={onSelect}
        iconRailClassName="isolate"
        iconTileClassName="icon-hover-pulse"
        // Selection is the whole point of these cards, so it reads as a state
        // on the card rather than a control tucked inside it.
        className={cn(
          "cursor-pointer text-left",
          selected && "border-foreground ring-foreground ring-1",
        )}
        overlay={
          <canvas
            ref={canvasRef}
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 -z-10 size-full"
          />
        }
        icon={<Icon className="text-foreground size-10" strokeWidth={1.25} />}
      >
        <Text
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary transition-colors"
        >
          {source.name}
        </Text>
        <Text small muted className="mt-1">
          {source.kind === "openapi" ? "OpenAPI document" : "Function"}
        </Text>
        {/* An explicit target for the choice: the ring alone reads as hover on
            a card that is already clickable everywhere. */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-3">
          {/* Named, not implicit: reading about a source is a different act
              from choosing it, and a bare card click hides that. */}
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onInspect();
            }}
            className="text-muted-foreground hover:text-foreground text-sm underline-offset-4 hover:underline"
          >
            Show details
          </button>
          <button
            type="button"
            onClick={(e) => {
              // Sits inside a card whose own click opens the panel.
              e.stopPropagation();
              onSelect();
            }}
            aria-pressed={selected}
            className="hover:text-foreground flex items-center gap-2"
          >
            <Text small muted={!selected}>
              {selected ? "Selected" : "Select"}
            </Text>
            {selected ? (
              <div className="bg-foreground flex size-5 items-center justify-center">
                <Check className="text-background size-3.5" strokeWidth={3} />
              </div>
            ) : (
              <div className="border-border size-5 border" />
            )}
          </button>
        </div>
      </Card.Entity>
    </div>
  );
}

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
  const { data: deploymentResult, isLoading: isLoadingDeployment } =
    useLatestDeployment();
  const { data: toolsResult, isLoading: isLoadingTools } = useListTools();

  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [showAll, setShowAll] = useState(false);
  const [name, setName] = useState("");
  const [isCreating, setIsCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const deployment = deploymentResult?.deployment;

  const sources: SourceOption[] = useMemo(() => {
    const openapi = (deployment?.openapiv3Assets ?? []).map((asset) => ({
      key: `openapi:${asset.id}`,
      name: asset.name,
      kind: "openapi" as const,
      documentId: asset.id,
    }));
    const functions = (deployment?.functionsAssets ?? []).map((asset) => ({
      key: `function:${asset.id}`,
      name: asset.name,
      kind: "function" as const,
      functionId: asset.id,
    }));
    return [...openapi, ...functions];
  }, [deployment]);

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
                      if (!name.trim()) setName(source.name);
                    }}
                    onInspect={() =>
                      openPanel({
                        kind: "source",
                        title: source.name,
                        subtitle: "Source",
                        props: {
                          sourceKind: source.kind,
                          assetId: source.documentId ?? source.functionId ?? "",
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
              onChange={setName}
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
