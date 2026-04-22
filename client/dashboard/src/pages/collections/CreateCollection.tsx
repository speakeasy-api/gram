import { Page } from "@/components/page-layout";
import { Textarea } from "@/components/moon/textarea";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import { useQueries } from "@tanstack/react-query";
import {
  Globe,
  Lock,
  Loader2,
  Search,
  Server as ServerIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useCreateCollection } from "./hooks";

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export default function CreateCollection() {
  const orgRoutes = useOrgRoutes();
  const client = useSdkClient();
  const organization = useOrganization();
  const projects = useMemo(
    () => organization.projects ?? [],
    [organization.projects],
  );

  const orgSlug = organization.slug ?? "";
  const baseNamespace = `com.speakeasy.${orgSlug}`;

  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);
  const [namespace, setNamespace] = useState(baseNamespace + ".");
  const [namespaceTouched, setNamespaceTouched] = useState(false);
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<"public" | "private">("private");
  const [selectedToolsetIds, setSelectedToolsetIds] = useState<Set<string>>(
    new Set(),
  );
  const [serverSearch, setServerSearch] = useState("");
  const createCollection = useCreateCollection();

  // Fetch toolsets from every project in the org
  const toolsetQueries = useQueries({
    queries: projects.map((project) => ({
      queryKey: ["toolsets", "list", project.slug],
      queryFn: () => client.toolsets.list({ gramProject: project.slug }),
      enabled: !!project.slug,
    })),
  });

  const toolsetsLoading = toolsetQueries.some((q) => q.isLoading);

  // Merge toolsets from all projects, excluding catalog-installed ones
  const toolsets = useMemo(() => {
    const all: Array<{
      id: string;
      name: string;
      description?: string;
      projectName: string;
      projectSlug: string;
    }> = [];
    for (let i = 0; i < projects.length; i++) {
      const project = projects[i];
      const data = toolsetQueries[i]?.data;
      for (const t of data?.toolsets ?? []) {
        if (t.toolUrns?.some((u) => u.startsWith("tools:externalmcp:")))
          continue;
        all.push({
          id: t.id,
          name: t.name,
          description: t.description ?? undefined,
          projectName: project.name,
          projectSlug: project.slug,
        });
      }
    }
    return all;
  }, [projects, toolsetQueries]);

  const filteredToolsets = useMemo(() => {
    if (!serverSearch) return toolsets;
    const q = serverSearch.toLowerCase();
    return toolsets.filter(
      (t) =>
        t.name.toLowerCase().includes(q) ||
        (t.description && t.description.toLowerCase().includes(q)) ||
        t.projectName.toLowerCase().includes(q),
    );
  }, [toolsets, serverSearch]);

  const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setName(e.target.value);
    const newSlug = slugify(e.target.value);
    if (!slugTouched) {
      setSlug(newSlug);
    }
    if (!namespaceTouched) {
      setNamespace(`${baseNamespace}.${slugTouched ? slug : newSlug}`);
    }
  };

  const toggleToolset = (id: string) => {
    setSelectedToolsetIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    const toolsetIds = Array.from(selectedToolsetIds);

    await createCollection.mutateAsync({
      request: {
        name,
        slug,
        mcpRegistryNamespace: namespace,
        description: description || undefined,
        visibility,
        toolsetIds: toolsetIds.length > 0 ? toolsetIds : undefined,
      },
    });
    orgRoutes.collections.goTo();
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Create Collection</Page.Section.Title>
          <Page.Section.Description>
            Create a curated collection of MCP servers that can be installed
            together
          </Page.Section.Description>
          <Page.Section.Body>
            <form onSubmit={handleSubmit} className="max-w-lg">
              <Stack direction="vertical" gap={4}>
                <div>
                  <label
                    htmlFor="name"
                    className="mb-1 block text-sm font-medium"
                  >
                    Name
                  </label>
                  <Input
                    id="name"
                    placeholder="e.g. Developer Productivity Suite"
                    value={name}
                    onChange={handleNameChange}
                    required
                  />
                </div>

                <div>
                  <label
                    htmlFor="slug"
                    className="mb-1 block text-sm font-medium"
                  >
                    Slug
                  </label>
                  <Input
                    id="slug"
                    placeholder="e.g. developer-productivity-suite"
                    value={slug}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => {
                      setSlug(e.target.value);
                      setSlugTouched(true);
                    }}
                    required
                  />
                </div>

                <div>
                  <label
                    htmlFor="namespace"
                    className="mb-1 block text-sm font-medium"
                  >
                    Registry Namespace
                  </label>
                  <Input
                    id="namespace"
                    placeholder={`${baseNamespace}.my-collection`}
                    value={namespace}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => {
                      setNamespace(e.target.value);
                      setNamespaceTouched(true);
                    }}
                    required
                  />
                  <p className="text-muted-foreground mt-1 text-xs">
                    Unique identifier used to address this collection in the
                    registry
                  </p>
                </div>

                <div>
                  <label
                    htmlFor="description"
                    className="mb-1 block text-sm font-medium"
                  >
                    Description
                  </label>
                  <Textarea
                    id="description"
                    placeholder="Describe what this collection is for and what servers it includes..."
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    rows={3}
                  />
                </div>

                <div>
                  <label className="mb-2 block text-sm font-medium">
                    Visibility
                  </label>
                  <div className="flex gap-2">
                    <button
                      type="button"
                      className={cn(
                        "flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm transition-colors",
                        visibility === "public"
                          ? "border-foreground/30 bg-accent"
                          : "border-border hover:bg-accent/50",
                      )}
                      onClick={() => setVisibility("public")}
                    >
                      <Globe className="h-3.5 w-3.5" />
                      Public
                    </button>
                    <button
                      type="button"
                      className={cn(
                        "flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm transition-colors",
                        visibility === "private"
                          ? "border-foreground/30 bg-accent"
                          : "border-border hover:bg-accent/50",
                      )}
                      onClick={() => setVisibility("private")}
                    >
                      <Lock className="h-3.5 w-3.5" />
                      Private
                    </button>
                  </div>
                  <p className="text-muted-foreground mt-1.5 text-xs">
                    {visibility === "private"
                      ? "Private collections are only visible to your organization."
                      : "Public collections are visible to everyone."}
                  </p>
                </div>

                <div>
                  <label className="mb-2 block text-sm font-medium">
                    MCP Servers ({selectedToolsetIds.size} selected)
                  </label>
                  <div className="rounded-md border">
                    <div className="relative border-b">
                      <Search className="text-muted-foreground absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2" />
                      <input
                        type="text"
                        placeholder="Search servers..."
                        value={serverSearch}
                        onChange={(e) => setServerSearch(e.target.value)}
                        className="placeholder:text-muted-foreground w-full bg-transparent py-2.5 pr-3 pl-9 text-sm outline-none"
                      />
                    </div>
                    <div className="max-h-64 overflow-y-auto">
                      {toolsetsLoading ? (
                        <div className="flex items-center justify-center p-4">
                          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
                        </div>
                      ) : filteredToolsets.length === 0 ? (
                        <div className="flex flex-col items-center justify-center p-4 text-center">
                          <ServerIcon className="text-muted-foreground mb-1 h-6 w-6" />
                          <Type small muted>
                            {serverSearch
                              ? "No servers match your search."
                              : "No MCP servers available."}
                          </Type>
                        </div>
                      ) : (
                        filteredToolsets.map((toolset) => (
                          <label
                            key={toolset.id}
                            className="hover:bg-accent/50 flex cursor-pointer items-start gap-3 border-b px-3 py-2.5 last:border-b-0"
                          >
                            <Checkbox
                              checked={selectedToolsetIds.has(toolset.id)}
                              onCheckedChange={() => toggleToolset(toolset.id)}
                              className="mt-0.5"
                            />
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="truncate text-sm font-medium">
                                  {toolset.name}
                                </span>
                                <Badge
                                  variant="secondary"
                                  className="shrink-0 text-xs"
                                >
                                  {toolset.projectName}
                                </Badge>
                              </div>
                              {toolset.description && (
                                <div className="text-muted-foreground mt-0.5 truncate text-xs">
                                  {toolset.description}
                                </div>
                              )}
                            </div>
                          </label>
                        ))
                      )}
                    </div>
                  </div>
                </div>

                <Stack direction="horizontal" gap={2}>
                  <Button
                    type="submit"
                    disabled={!name || !slug || createCollection.isPending}
                  >
                    <Button.Text>
                      {createCollection.isPending
                        ? "Creating..."
                        : "Create Collection"}
                    </Button.Text>
                  </Button>
                  <Button
                    variant="secondary"
                    type="button"
                    onClick={() => orgRoutes.collections.goTo()}
                  >
                    <Button.Text>Cancel</Button.Text>
                  </Button>
                </Stack>
              </Stack>
            </form>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
