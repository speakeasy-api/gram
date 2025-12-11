import { Page } from "@/components/page-layout";
import { ServerCard } from "@/components/server-card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@/components/ui/select";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { asTool } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { Toolset, ToolsetEntry } from "@gram/client/models/components";
import { useListToolsets } from "@gram/client/react-query";
import {
  ArrowUpIcon,
  BookOpenIcon,
  EditIcon,
  MessageCircleIcon,
  PencilRulerIcon,
  PlusIcon,
  SearchIcon,
  TrashIcon,
} from "lucide-react";
import {
  AnimatePresence,
  motion,
  useAnimationControls,
  useMotionValue,
} from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { Navigate, useNavigate, useSearchParams } from "react-router";
import { useMcpUrl } from "../mcp/MCPDetails";
import { MCPEmptyState } from "../mcp/MCPEmptyState";
import { START_PATH_PARAM, START_STEP_PARAM } from "../onboarding/Wizard";
import { useCloneToolset } from "../toolsets/Toolset";

export const LINKED_FROM_PARAM = "from";

// Component for toolset dropdown items
function ToolsetSelectItem({ toolset }: { toolset: ToolsetEntry }) {
  const { url } = useMcpUrl(toolset);
  return (
    <SelectItem value={toolset.slug}>
      <div className="flex flex-col items-start gap-0.5">
        <span className="font-medium">{toolset.name}</span>
        <code className="text-xs text-muted-foreground font-mono">
          {url || `mcp://${toolset.slug}`}
        </code>
      </div>
    </SelectItem>
  );
}

const useAllToolsets = (toolsetSlugs: string[]) => {
  const [toolsets, setToolsets] = useState<Toolset[]>([]);
  const [loading, setLoading] = useState(false);
  const client = useSdkClient();

  useEffect(() => {
    if (toolsetSlugs.length === 0) {
      setToolsets([]);
      return;
    }

    setLoading(true);

    const fetchAllToolsets = async () => {
      try {
        const promises = toolsetSlugs.map((slug) =>
          client.toolsets.getBySlug({ slug }),
        );

        const results = await Promise.allSettled(promises);
        const successfulResults = results
          .filter(
            (result): result is PromiseFulfilledResult<Toolset> =>
              result.status === "fulfilled",
          )
          .map((result) => result.value);

        setToolsets(successfulResults);
      } catch (error) {
        console.error("Failed to fetch toolsets:", error);
        setToolsets([]);
      } finally {
        setLoading(false);
      }
    };

    fetchAllToolsets();
  }, [toolsetSlugs, client]);

  return { toolsets, loading };
};

export default function Home() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="pb-8">
          <HomeContent />
        </div>
      </Page.Body>
    </Page>
  );
}

export const onboardingStepStorageKeys = {
  test: "onboarding_playground_completed",
  curate: "onboarding_toolsets_completed",
  configure: "onboarding_mcp_config_completed",
};

const iconComponents = {
  search: SearchIcon,
  plus: PlusIcon,
  trash: TrashIcon,
  edit: EditIcon,
  tool: PencilRulerIcon,
};

const colorClasses = {
  red: "text-red-500",
  green: "text-green-500",
  blue: "text-blue-500",
  yellow: "text-yellow-500",
};

// Map HTTP methods to icons and colors
const getHttpMethodIconAndColor = (method?: string) => {
  switch (method?.toUpperCase()) {
    case "GET":
      return { icon: "search", color: "blue" };
    case "POST":
      return { icon: "plus", color: "green" };
    case "PUT":
    case "PATCH":
      return { icon: "edit", color: "yellow" };
    case "DELETE":
      return { icon: "trash", color: "red" };
    default:
      return { icon: "tool", color: "blue" };
  }
};

function HomeContent() {
  const { data: toolsets } = useListToolsets();
  const routes = useRoutes();
  const navigate = useNavigate();
  const telemetry = useTelemetry();
  const cloneToolset = useCloneToolset();
  const [prompt, setPrompt] = useState("");
  const [isInputHovered, setIsInputHovered] = useState(false);
  const [isInputFocused, setIsInputFocused] = useState(false);
  const x = useMotionValue(0);
  const controls = useAnimationControls();
  const [selectedToolset, setSelectedToolset] = useState<string>("");
  const [hasStarted, setHasStarted] = useState(false);

  const [searchParams] = useSearchParams();
  const linkedFrom = searchParams.get(LINKED_FROM_PARAM);

  useEffect(() => {
    if (toolsets?.toolsets?.length && !selectedToolset) {
      setSelectedToolset(toolsets.toolsets[0]?.slug || "");
    }
  }, [toolsets?.toolsets, selectedToolset]);

  const selectedToolsetData = useMemo(
    () => toolsets?.toolsets?.find((t) => t.slug === selectedToolset),
    [toolsets?.toolsets, selectedToolset],
  );
  const { url: mcpUrl } = useMcpUrl(selectedToolsetData);

  const toolsetSlugs = useMemo(
    () => toolsets?.toolsets?.map((t) => t.slug) || [],
    [toolsets?.toolsets],
  );
  const { toolsets: allFullToolsets } = useAllToolsets(toolsetSlugs);

  const getToolIcon = (method?: string) => {
    const { icon } = getHttpMethodIconAndColor(method);
    return (
      iconComponents[icon as keyof typeof iconComponents] || PencilRulerIcon
    );
  };

  const getToolIconColor = (method?: string) => {
    const { color } = getHttpMethodIconAndColor(method);
    return (
      colorClasses[color as keyof typeof colorClasses] || colorClasses.blue
    );
  };

  const allToolDefinitions = useMemo(() => {
    if (allFullToolsets.length === 0) {
      const basicTools =
        toolsets?.toolsets?.flatMap(
          (toolset) =>
            toolset.tools?.map((tool) => ({
              name: tool.name,
              method: undefined,
            })) || [],
        ) || [];
      return basicTools;
    }

    const fullTools = allFullToolsets.flatMap(
      (toolset) =>
        toolset?.tools?.map(asTool).map((tool) => ({
          name: tool.name,
          method: tool.type === "http" ? tool.httpMethod : undefined,
        })) || [],
    );

    // Create R-G-B-Y color pattern by grouping tools by HTTP method
    const methodGroups = {
      DELETE: fullTools
        .filter((t) => t.method?.toUpperCase() === "DELETE")
        .sort((a, b) => a.name.localeCompare(b.name)),
      PUT_PATCH: fullTools
        .filter((t) => ["PUT", "PATCH"].includes(t.method?.toUpperCase() || ""))
        .sort((a, b) => a.name.localeCompare(b.name)),
      POST: fullTools
        .filter((t) => t.method?.toUpperCase() === "POST")
        .sort((a, b) => a.name.localeCompare(b.name)),
      GET: fullTools
        .filter((t) => t.method?.toUpperCase() === "GET")
        .sort((a, b) => a.name.localeCompare(b.name)),
      OTHER: fullTools
        .filter(
          (t) =>
            !["DELETE", "PUT", "PATCH", "POST", "GET"].includes(
              t.method?.toUpperCase() || "",
            ),
        )
        .sort((a, b) => a.name.localeCompare(b.name)),
    };

    const sortedTools = [];
    const maxLength = Math.max(
      methodGroups.DELETE.length,
      methodGroups.PUT_PATCH.length,
      methodGroups.POST.length,
      methodGroups.GET.length,
      methodGroups.OTHER.length,
    );

    for (let i = 0; i < maxLength; i++) {
      if (methodGroups.DELETE[i]) sortedTools.push(methodGroups.DELETE[i]);
      if (methodGroups.PUT_PATCH[i])
        sortedTools.push(methodGroups.PUT_PATCH[i]);
      if (methodGroups.POST[i]) sortedTools.push(methodGroups.POST[i]);
      if (methodGroups.GET[i]) sortedTools.push(methodGroups.GET[i]);
      if (methodGroups.OTHER[i]) sortedTools.push(methodGroups.OTHER[i]);
    }

    return sortedTools;
  }, [allFullToolsets, toolsets?.toolsets]);

  const duplicatedSuggestions = useMemo(() => {
    const toolsWithIcons = allToolDefinitions
      .filter((tool): tool is NonNullable<typeof tool> => Boolean(tool))
      .map((tool) => ({
        name: tool.name,
        method: tool.method,
        IconComponent: getToolIcon(tool.method),
        iconColorClass: getToolIconColor(tool.method),
      }));
    return [...toolsWithIcons, ...toolsWithIcons];
  }, [allToolDefinitions]);

  const totalToolCount = useMemo(
    () =>
      toolsets?.toolsets?.reduce(
        (acc, toolset) => acc + (toolset.tools?.length || 0),
        0,
      ) || 0,
    [toolsets?.toolsets],
  );

  const shouldAnimate = !isInputHovered && !isInputFocused;
  const animationDuration = Math.max(45, totalToolCount * 3.5);
  useEffect(() => {
    if (!toolsets?.toolsets || toolsets.toolsets.length === 0) return;

    if (!hasStarted && animationDuration > 0) {
      controls.start({
        x: [0, "-50%"],
        transition: {
          repeat: Infinity,
          repeatType: "loop",
          duration: animationDuration,
          ease: "linear",
        },
      });
      setHasStarted(true);
      return;
    }

    if (hasStarted) {
      if (shouldAnimate) {
        controls.start({
          x: [x.get(), "-50%"],
          transition: {
            repeat: Infinity,
            repeatType: "loop",
            duration: animationDuration,
            ease: "linear",
          },
        });
      } else {
        controls.stop();
      }
    }
  }, [
    shouldAnimate,
    toolsets?.toolsets,
    controls,
    x,
    animationDuration,
    hasStarted,
  ]);

  // If we arrived here from the CLI and the user has no toolsets, redirect to the onboarding page.
  if (linkedFrom === "cli" && toolsets?.toolsets?.length === 0) {
    const params = new URLSearchParams();
    params.set(START_PATH_PARAM, "cli");
    params.set(START_STEP_PARAM, "toolset");

    return <Navigate to={`${routes.onboarding.href()}?${params.toString()}`} />;
  }

  if (toolsets?.toolsets.length === 0) {
    return <MCPEmptyState />;
  }

  const handleQuickTest = () => {
    if (!prompt.trim() || !selectedToolset) return;

    telemetry.capture("home_action", {
      action: "quick_test",
      hasPrompt: true,
      toolset: selectedToolset,
    });

    // Navigate to playground with toolset and prompt as URL parameters
    const params = new URLSearchParams();
    params.set("toolset", selectedToolset);
    params.set("prompt", prompt.trim());

    navigate(routes.playground.href() + "?" + params.toString());
  };

  return (
    <>
      {/* Quick Test Section */}
      <Page.Section>
        <Page.Section.Title>Chat with your MCP server</Page.Section.Title>
        <Page.Section.Description>
          Start a conversation and watch your tools come to life
        </Page.Section.Description>
        <Page.Section.Body>
          <div
            className="relative rounded-lg border px-4 py-4 space-y-3 cursor-pointer"
            onMouseEnter={() => setIsInputHovered(true)}
            onMouseLeave={() => setIsInputHovered(false)}
          >
            <div className="relative">
              <textarea
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder="Chat with your MCP server..."
                className="w-full text-sm min-h-[48px] p-0 bg-transparent border-0 shadow-none rounded-none resize-none ring-0 outline-none focus-visible:ring-0 focus-visible:border-0"
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    handleQuickTest();
                  }
                }}
                onFocus={() => setIsInputFocused(true)}
                onBlur={() => setIsInputFocused(false)}
              />
            </div>

            <div className="h-6 flex items-center relative">
              <AnimatePresence mode="wait">
                {!prompt.trim() ? (
                  <motion.div
                    key="suggestions"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="absolute inset-x-0 top-0 bottom-0 overflow-hidden"
                    style={{
                      right: "-16px",
                      maskImage:
                        "linear-gradient(to right, transparent 0%, black 16px, black calc(100% - 16px), transparent 100%)",
                      WebkitMaskImage:
                        "linear-gradient(to right, transparent 0%, black 16px, black calc(100% - 16px), transparent 100%)",
                    }}
                  >
                    <motion.div
                      className="flex gap-2"
                      style={{ width: "max-content", x }}
                      animate={controls}
                      initial={{ x: 0 }}
                    >
                      {duplicatedSuggestions
                        .map((tool, index) => {
                          if (!tool) return null;

                          return (
                            <Badge
                              key={`${tool.name}-${index}`}
                              variant="outline"
                              size="sm"
                              className="flex-shrink-0 font-mono uppercase flex items-center gap-1.5"
                            >
                              <tool.IconComponent
                                className={`w-3 h-3 ${tool.iconColorClass}`}
                              />
                              {tool.name}
                            </Badge>
                          );
                        })
                        .filter(Boolean)}
                    </motion.div>
                  </motion.div>
                ) : (
                  <motion.div
                    key="controls"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="flex justify-between items-center w-full gap-2"
                  >
                    <div className="flex-1 min-w-0">
                      <Select
                        value={selectedToolset}
                        onValueChange={setSelectedToolset}
                      >
                        <SelectTrigger className="h-6 text-xs border-0 shadow-none bg-transparent px-0 py-0 focus:ring-0 w-fit min-w-0 justify-start">
                          {selectedToolset ? (
                            <div className="flex items-center gap-1.5">
                              <svg
                                fill="currentColor"
                                fillRule="evenodd"
                                height="1em"
                                style={{ flex: "none", lineHeight: 1 }}
                                viewBox="0 0 24 24"
                                width="1em"
                                xmlns="http://www.w3.org/2000/svg"
                                className="w-3 h-3 text-muted-foreground"
                              >
                                <path d="M15.688 2.343a2.588 2.588 0 00-3.61 0l-9.626 9.44a.863.863 0 01-1.203 0 .823.823 0 010-1.18l9.626-9.44a4.313 4.313 0 016.016 0 4.116 4.116 0 011.204 3.54 4.3 4.3 0 013.609 1.18l.05.05a4.115 4.115 0 010 5.9l-8.706 8.537a.274.274 0 000 .393l1.788 1.754a.823.823 0 010 1.18.863.863 0 01-1.203 0l-1.788-1.753a1.92 1.92 0 010-2.754l8.706-8.538a2.47 2.47 0 000-3.54l-.05-.049a2.588 2.588 0 00-3.607-.003l-7.172 7.034-.002.002-.098.097a.863.863 0 01-1.204 0 .823.823 0 010-1.18l7.273-7.133a2.47 2.47 0 00-.003-3.537z"></path>
                                <path d="M14.485 4.703a.823.823 0 000-1.18.863.863 0 00-1.204 0l-7.119 6.982a4.115 4.115 0 000 5.9 4.314 4.314 0 006.016 0l7.12-6.982a.823.823 0 000-1.18.863.863 0 00-1.204 0l-7.119 6.982a2.588 2.588 0 01-3.61 0 2.47 2.47 0 010-3.54l7.12-6.982z"></path>
                              </svg>
                              <code className="font-mono text-foreground truncate">
                                {mcpUrl || `mcp://${selectedToolset}`}
                              </code>
                            </div>
                          ) : (
                            <span className="text-muted-foreground">
                              Loading...
                            </span>
                          )}
                        </SelectTrigger>
                        <SelectContent>
                          {toolsets?.toolsets?.map((toolset) => (
                            <ToolsetSelectItem
                              key={toolset.slug}
                              toolset={toolset}
                            />
                          ))}
                        </SelectContent>
                      </Select>
                    </div>

                    <Button
                      onClick={handleQuickTest}
                      size="icon"
                      className="rounded w-6 h-6 bg-[#232323] shadow-[0px_1px_1px_0px_inset_rgba(255,255,255,0.24),0px_-1px_1px_0px_inset_rgba(0,0,0,0.64)]"
                      disabled={!selectedToolset}
                    >
                      <ArrowUpIcon className="w-3 h-3" />
                    </Button>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>
          </div>
        </Page.Section.Body>
      </Page.Section>

      {/* Actions Section */}
      <Page.Section>
        <Page.Section.Title>Explore your tools</Page.Section.Title>
        <Page.Section.Description>
          Dive deeper into testing, building, and managing your MCP servers
        </Page.Section.Description>
        <Page.Section.Body>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <Button
              variant="outline"
              size="lg"
              onClick={() => {
                telemetry.capture("home_action", {
                  action: "global_action",
                  button: "test_playground",
                });
                routes.playground.goTo();
              }}
              className="rounded-lg h-auto flex flex-col items-start justify-start gap-4 p-4 text-base overflow-hidden"
            >
              <MessageCircleIcon className="w-8 h-8 flex-shrink-0" />
              <div className="flex flex-col items-start text-left gap-0.5">
                <span className="font-normal text-sm leading-tight">
                  Test in Playground
                </span>
                <span className="text-xs opacity-80 font-normal leading-tight">
                  Try your tools with AI models
                </span>
              </div>
            </Button>

            <Button
              variant="outline"
              size="lg"
              onClick={() => {
                telemetry.capture("home_action", {
                  action: "global_action",
                  button: "manage_toolsets",
                });
                routes.toolsets.goTo();
              }}
              className="rounded-lg h-auto flex flex-col items-start justify-start gap-4 p-4 text-base overflow-hidden"
            >
              <BookOpenIcon className="w-8 h-8 flex-shrink-0" />
              <div className="flex flex-col items-start text-left gap-0.5">
                <span className="font-normal text-sm leading-tight">
                  Manage Toolsets
                </span>
                <span className="text-xs opacity-80 font-normal leading-tight">
                  Edit your API collections
                </span>
              </div>
            </Button>

            <Button
              variant="outline"
              size="lg"
              onClick={() => {
                telemetry.capture("home_action", {
                  action: "global_action",
                  button: "create_custom_tools",
                });
                routes.customTools.goTo();
              }}
              className="rounded-lg h-auto flex flex-col items-start justify-start gap-4 p-4 text-base overflow-hidden"
            >
              <PencilRulerIcon className="w-8 h-8 flex-shrink-0" />
              <div className="flex flex-col items-start text-left gap-0.5">
                <span className="font-normal text-sm leading-tight">
                  Build Custom Tools
                </span>
                <span className="text-xs opacity-80 font-normal leading-tight">
                  Create tools from scratch
                </span>
              </div>
            </Button>
          </div>
        </Page.Section.Body>
      </Page.Section>

      {/* MCP Servers Section */}
      <Page.Section>
        <Page.Section.Title>Your servers</Page.Section.Title>
        <Page.Section.Description>
          Manage settings, privacy, and configurations for your MCP servers
        </Page.Section.Description>
        <Page.Section.Body>
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {toolsets?.toolsets?.map((toolset) => (
              <ServerCard
                key={toolset.slug}
                toolset={toolset}
                className="bg-secondary"
                additionalActions={[
                  {
                    label: "Clone",
                    onClick: () => cloneToolset(toolset.slug),
                    icon: "copy",
                  },
                ]}
              />
            ))}
          </div>
        </Page.Section.Body>
      </Page.Section>
    </>
  );
}
