import { Page } from "@/components/page-layout";
import { useSlugs } from "@/contexts/Sdk";
import { useProject, useSession } from "@/contexts/Auth";
import { cn, getServerURL } from "@/lib/utils";
import { useChatSessionsCreateMutation } from "@gram/client/react-query/chatSessionsCreate";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { useListToolsets } from "@gram/client/react-query/index.js";
import { useEffect, useMemo, useRef, useState } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { TextArea } from "@/components/ui/textarea";
import {
  ArrowRight,
  Check,
  ChevronDown,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Server,
} from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  CodeBlock,
  CodeBlockCopyButton,
} from "@/components/ai-elements/code-block";
import { useRoutes } from "@/routes";

type ColorScheme = "light" | "dark" | "system";
type Density = "compact" | "normal" | "spacious";
type Radius = "round" | "soft" | "sharp";
type Variant = "widget" | "sidecar" | "standalone";
type ModalPosition = "bottom-right" | "bottom-left" | "top-right" | "top-left";

interface ElementsFormConfig {
  // Connection
  mcp: string;
  // Theme
  colorScheme: ColorScheme;
  density: Density;
  radius: Radius;
  variant: Variant;
  // Welcome
  welcomeTitle: string;
  welcomeSubtitle: string;
  // Composer
  composerPlaceholder: string;
  // Model
  showModelPicker: boolean;
  // System Prompt
  systemPrompt: string;
  // Modal (widget variant)
  modalTitle: string;
  modalPosition: ModalPosition;
  modalDefaultOpen: boolean;
  // Tools
  expandToolGroupsByDefault: boolean;
}

const defaultConfig: ElementsFormConfig = {
  mcp: "",
  colorScheme: "system",
  density: "normal",
  radius: "soft",
  variant: "standalone",
  welcomeTitle: "Welcome",
  welcomeSubtitle: "How can I help you today?",
  composerPlaceholder: "Send a message...",
  showModelPicker: false,
  systemPrompt: "",
  modalTitle: "Chat",
  modalPosition: "bottom-right",
  modalDefaultOpen: false,
  expandToolGroupsByDefault: false,
};

function ConfigSection({
  title,
  children,
  isOpen,
  onToggle,
}: {
  title: string;
  children: React.ReactNode;
  isOpen: boolean;
  onToggle: () => void;
}) {
  return (
    <div>
      <button
        onClick={onToggle}
        className="flex items-center justify-between w-full group"
      >
        <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
          {title}
        </h3>
        <motion.div
          animate={{ rotate: isOpen ? 0 : -90 }}
          transition={{ duration: 0.2, ease: "easeInOut" }}
        >
          <ChevronDown className="h-4 w-4 text-muted-foreground" />
        </motion.div>
      </button>
      <AnimatePresence initial={false}>
        {isOpen && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2, ease: "easeInOut" }}
            className="overflow-hidden"
          >
            <div className="pt-3 space-y-4">{children}</div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function ConfigField({
  label,
  description,
  children,
}: {
  label: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <Label className="text-sm font-medium">{label}</Label>
      {description && (
        <p className="text-xs text-muted-foreground">{description}</p>
      )}
      {children}
    </div>
  );
}

function SwitchField({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string;
  description?: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div className="space-y-0.5">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

export default function ChatElements() {
  const { projectSlug } = useSlugs();
  const session = useSession();
  const project = useProject();
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [iframeReady, setIframeReady] = useState(false);
  const [iframeKey, setIframeKey] = useState(0);
  const [sessionToken, setSessionToken] = useState<string | null>(null);
  const sessionCreatedRef = useRef(false);
  const [rightPanelTab, setRightPanelTab] = useState<"preview" | "install">(
    "preview",
  );

  const [config, setConfig] = useState<ElementsFormConfig>(defaultConfig);
  const [openSection, setOpenSection] = useState<string | null>("connection");
  const routes = useRoutes();

  // Load available MCP servers (toolsets)
  const { data: toolsetsData, isLoading: toolsetsLoading } = useListToolsets();
  const toolsets = useMemo(
    () =>
      toolsetsData?.toolsets.sort((a, b) => a.name.localeCompare(b.name)) || [],
    [toolsetsData],
  );

  // Helper to get MCP URL for a toolset
  const getMcpUrl = (toolset: (typeof toolsets)[0]) => {
    const urlSuffix = toolset.mcpSlug
      ? toolset.mcpSlug
      : `${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug}`;
    return `${getServerURL()}/mcp/${urlSuffix}`;
  };

  // Set first available toolset as default when toolsets load
  useEffect(() => {
    if (toolsets.length > 0 && !config.mcp) {
      const firstEnabledToolset = toolsets.find((t) => t.mcpEnabled);
      if (firstEnabledToolset) {
        updateConfig("mcp", getMcpUrl(firstEnabledToolset));
      }
    }
  }, [toolsets]);

  const updateConfig = <K extends keyof ElementsFormConfig>(
    key: K,
    value: ElementsFormConfig[K],
  ) => {
    setConfig((prev) => {
      const newConfig = { ...prev, [key]: value };

      // Auto-set modalDefaultOpen to true when switching to widget variant
      if (key === "variant" && value === "widget") {
        newConfig.modalDefaultOpen = true;
      }

      return newConfig;
    });
  };

  const createSessionMutation = useChatSessionsCreateMutation();

  // Listen for iframe ready signal
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return;
      if (event.data?.type === "ELEMENTS_READY") {
        setIframeReady(true);
      }
    };

    window.addEventListener("message", handleMessage);
    return () => window.removeEventListener("message", handleMessage);
  }, []);

  // Create session ONCE when iframe is ready
  useEffect(() => {
    if (!iframeReady || sessionCreatedRef.current) return;
    sessionCreatedRef.current = true;

    const createSession = async () => {
      try {
        const result = await createSessionMutation.mutateAsync({
          security: {
            option1: {
              projectSlugHeaderGramProject: project.slug,
              sessionHeaderGramSession: session.session,
            },
          },
          request: {
            createRequestBody: {
              embedOrigin: window.location.origin,
            },
          },
        });
        setSessionToken(result.clientToken);
      } catch (error) {
        console.error("Failed to create session:", error);
      }
    };

    createSession();
  }, [iframeReady, createSessionMutation, project.slug, session.session]);

  // Build config object (memoized)
  const elementsConfig = useMemo(
    () => ({
      mcp: config.mcp,
      projectSlug: project.slug,
      apiUrl: getServerURL(),
      systemPrompt: config.systemPrompt || undefined,
      variant: config.variant,
      theme: {
        colorScheme: config.colorScheme,
        density: config.density,
        radius: config.radius,
      },
      welcome: {
        title: config.welcomeTitle,
        subtitle: config.welcomeSubtitle,
      },
      composer: {
        placeholder: config.composerPlaceholder,
      },
      model: {
        showModelPicker: config.showModelPicker,
      },
      sidecar: {
        expandable: false,
      },
      modal: {
        title: config.modalTitle,
        position: config.modalPosition,
        defaultOpen: config.modalDefaultOpen,
      },
      tools: {
        expandToolGroupsByDefault: config.expandToolGroupsByDefault,
      },
    }),
    [config, project.slug],
  );

  // Send config to iframe whenever it changes (after session is ready)
  useEffect(() => {
    if (!iframeReady || !sessionToken || !iframeRef.current?.contentWindow)
      return;

    const iframe = iframeRef.current.contentWindow;
    iframe.postMessage(
      { type: "ELEMENTS_CONFIG", config: elementsConfig },
      window.location.origin,
    );
  }, [iframeReady, sessionToken, elementsConfig]);

  // Send session token to iframe when available
  useEffect(() => {
    if (!iframeReady || !sessionToken || !iframeRef.current?.contentWindow)
      return;

    const iframe = iframeRef.current.contentWindow;
    iframe.postMessage(
      { type: "ELEMENTS_SESSION", sessionToken },
      window.location.origin,
    );
  }, [iframeReady, sessionToken]);

  const refreshPreview = () => {
    setIframeReady(false);
    setSessionToken(null);
    sessionCreatedRef.current = false;
    setIframeKey((k) => k + 1);
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Chat Elements</Page.Section.Title>
          <Page.Section.Description>
            Embeddable AI chat experience for your applications
          </Page.Section.Description>
          <Page.Section.CTA>
            <Tabs
              value={rightPanelTab}
              onValueChange={(v) =>
                setRightPanelTab(v as "preview" | "install")
              }
            >
              <TabsList>
                <TabsTrigger value="preview">Preview</TabsTrigger>
                <TabsTrigger value="install">Installation</TabsTrigger>
              </TabsList>
            </Tabs>
          </Page.Section.CTA>
          <Page.Section.Body>
            <div className="mt-3">
              {/* Preview Mode */}
              <div
                className={`${rightPanelTab === "preview" ? "block" : "hidden"}`}
              >
                <div className="flex gap-12 min-h-fit">
                  {/* Config Panel */}

                  <div className="w-1/3 h-full overflow-y-auto pr-4 space-y-6">
                    {/* Connection */}
                    <ConfigSection
                      title="MCP"
                      isOpen={openSection === "connection"}
                      onToggle={() =>
                        setOpenSection((prev) =>
                          prev === "connection" ? null : "connection",
                        )
                      }
                    >
                      <ConfigField
                        label="Connected Server"
                        description="The chosen server's tools will be loaded into the chat context"
                      >
                        <div className="space-y-3">
                          {toolsetsLoading ? (
                            <div className="text-sm text-muted-foreground">
                              Loading MCP servers...
                            </div>
                          ) : toolsets.length === 0 ? (
                            <div className="text-sm text-muted-foreground">
                              No MCP servers available. Add a source to create
                              one.
                            </div>
                          ) : (
                            <Select
                              value={config.mcp}
                              onValueChange={(v) => updateConfig("mcp", v)}
                            >
                              <SelectTrigger className="w-full">
                                <SelectValue placeholder="Select an MCP server" />
                              </SelectTrigger>
                              <SelectContent>
                                {toolsets.map((toolset) => {
                                  const mcpUrl = getMcpUrl(toolset);
                                  return (
                                    <SelectItem
                                      key={toolset.id}
                                      value={mcpUrl}
                                      disabled={!toolset.mcpEnabled}
                                    >
                                      <div className="flex items-center gap-2">
                                        <Server className="h-4 w-4 text-muted-foreground" />
                                        <span>{toolset.name}</span>
                                        {!toolset.mcpEnabled && (
                                          <span className="text-xs text-muted-foreground">
                                            (disabled)
                                          </span>
                                        )}
                                      </div>
                                    </SelectItem>
                                  );
                                })}
                              </SelectContent>
                            </Select>
                          )}
                          <Button
                            variant="outline"
                            size="sm"
                            className="w-full"
                            onClick={() => routes.toolsets.goTo()}
                          >
                            <Plus className="h-4 w-4 mr-2" />
                            Add New MCP Server
                          </Button>
                        </div>
                      </ConfigField>
                    </ConfigSection>

                    {/* Appearance */}
                    <ConfigSection
                      title="Appearance"
                      isOpen={openSection === "appearance"}
                      onToggle={() =>
                        setOpenSection((prev) =>
                          prev === "appearance" ? null : "appearance",
                        )
                      }
                    >
                      <ConfigField label="Variant" description="Layout style">
                        <Select
                          value={config.variant}
                          onValueChange={(v) =>
                            updateConfig("variant", v as Variant)
                          }
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="standalone">
                              Standalone
                            </SelectItem>
                            <SelectItem value="widget">Widget</SelectItem>
                            <SelectItem value="sidecar">Sidecar</SelectItem>
                          </SelectContent>
                        </Select>
                      </ConfigField>

                      <ConfigField
                        label="Color Scheme"
                        description="The color scheme of the chat"
                      >
                        <Select
                          value={config.colorScheme}
                          onValueChange={(v) =>
                            updateConfig("colorScheme", v as ColorScheme)
                          }
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="system">System</SelectItem>
                            <SelectItem value="light">Light</SelectItem>
                            <SelectItem value="dark">Dark</SelectItem>
                          </SelectContent>
                        </Select>
                      </ConfigField>

                      <ConfigField
                        label="Density"
                        description="Spacing density"
                      >
                        <Select
                          value={config.density}
                          onValueChange={(v) =>
                            updateConfig("density", v as Density)
                          }
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="compact">Compact</SelectItem>
                            <SelectItem value="normal">Normal</SelectItem>
                            <SelectItem value="spacious">Spacious</SelectItem>
                          </SelectContent>
                        </Select>
                      </ConfigField>

                      <ConfigField
                        label="Border Radius"
                        description="The border radius size for UI elements within the chat"
                      >
                        <Select
                          value={config.radius}
                          onValueChange={(v) =>
                            updateConfig("radius", v as Radius)
                          }
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="round">Round</SelectItem>
                            <SelectItem value="soft">Soft</SelectItem>
                            <SelectItem value="sharp">Sharp</SelectItem>
                          </SelectContent>
                        </Select>
                      </ConfigField>
                    </ConfigSection>

                    {/* Welcome */}
                    <ConfigSection
                      title="Welcome Screen"
                      isOpen={openSection === "welcome"}
                      onToggle={() =>
                        setOpenSection((prev) =>
                          prev === "welcome" ? null : "welcome",
                        )
                      }
                    >
                      <ConfigField
                        label="Title"
                        description="The title to show on the welcome screen"
                      >
                        <Input
                          value={config.welcomeTitle}
                          onChange={(value) =>
                            updateConfig("welcomeTitle", value)
                          }
                          placeholder="Welcome"
                        />
                      </ConfigField>
                      <ConfigField
                        label="Subtitle"
                        description="The subtitle to show on the welcome screen"
                      >
                        <Input
                          value={config.welcomeSubtitle}
                          onChange={(value) =>
                            updateConfig("welcomeSubtitle", value)
                          }
                          placeholder="How can I help you today?"
                        />
                      </ConfigField>
                    </ConfigSection>

                    {/* Composer */}
                    <ConfigSection
                      title="Composer"
                      isOpen={openSection === "composer"}
                      onToggle={() =>
                        setOpenSection((prev) =>
                          prev === "composer" ? null : "composer",
                        )
                      }
                    >
                      <ConfigField
                        label="Placeholder"
                        description="The placeholder text for the composer input"
                      >
                        <Input
                          value={config.composerPlaceholder}
                          onChange={(value) =>
                            updateConfig("composerPlaceholder", value)
                          }
                          placeholder="Send a message..."
                        />
                      </ConfigField>
                    </ConfigSection>

                    {/* Model */}
                    <ConfigSection
                      title="Model"
                      isOpen={openSection === "model"}
                      onToggle={() =>
                        setOpenSection((prev) =>
                          prev === "model" ? null : "model",
                        )
                      }
                    >
                      <SwitchField
                        label="Show Model Picker"
                        description="Allow users to select different models"
                        checked={config.showModelPicker}
                        onCheckedChange={(checked) =>
                          updateConfig("showModelPicker", checked)
                        }
                      />
                    </ConfigSection>

                    {/* System Prompt */}
                    <ConfigSection
                      title="System Prompt"
                      isOpen={openSection === "systemPrompt"}
                      onToggle={() =>
                        setOpenSection((prev) =>
                          prev === "systemPrompt" ? null : "systemPrompt",
                        )
                      }
                    >
                      <ConfigField
                        label="Instructions"
                        description="Custom instructions for the AI assistant"
                      >
                        <TextArea
                          value={config.systemPrompt}
                          onChange={(value) =>
                            updateConfig("systemPrompt", value)
                          }
                          placeholder="You are a helpful assistant..."
                          rows={4}
                        />
                      </ConfigField>
                    </ConfigSection>

                    {/* Modal Config (only for widget variant) */}
                    {config.variant === "widget" && (
                      <ConfigSection
                        title="Widget Modal"
                        isOpen={openSection === "widgetModal"}
                        onToggle={() =>
                          setOpenSection((prev) =>
                            prev === "widgetModal" ? null : "widgetModal",
                          )
                        }
                      >
                        <ConfigField label="Modal Title">
                          <Input
                            value={config.modalTitle}
                            onChange={(value) =>
                              updateConfig("modalTitle", value)
                            }
                            placeholder="Chat"
                          />
                        </ConfigField>
                        <ConfigField label="Position">
                          <Select
                            value={config.modalPosition}
                            onValueChange={(v) =>
                              updateConfig("modalPosition", v as ModalPosition)
                            }
                          >
                            <SelectTrigger className="w-full">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="bottom-right">
                                Bottom Right
                              </SelectItem>
                              <SelectItem value="bottom-left">
                                Bottom Left
                              </SelectItem>
                              <SelectItem value="top-right">
                                Top Right
                              </SelectItem>
                              <SelectItem value="top-left">Top Left</SelectItem>
                            </SelectContent>
                          </Select>
                        </ConfigField>
                        <SwitchField
                          label="Open by Default"
                          checked={config.modalDefaultOpen}
                          onCheckedChange={(checked) =>
                            updateConfig("modalDefaultOpen", checked)
                          }
                        />
                      </ConfigSection>
                    )}

                    {/* Tools */}
                    <ConfigSection
                      title="Tools"
                      isOpen={openSection === "tools"}
                      onToggle={() =>
                        setOpenSection((prev) =>
                          prev === "tools" ? null : "tools",
                        )
                      }
                    >
                      <SwitchField
                        label="Expand Tool Groups by Default"
                        description="Show tool call details expanded"
                        checked={config.expandToolGroupsByDefault}
                        onCheckedChange={(checked) =>
                          updateConfig("expandToolGroupsByDefault", checked)
                        }
                      />
                    </ConfigSection>
                  </div>

                  {/* Preview Panel */}
                  <div className="w-2/3 flex flex-col h-[700px]">
                    <div className="relative flex-1 rounded-lg border overflow-hidden bg-muted/30">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={refreshPreview}
                        className={cn(
                          "absolute top-6 right-6 z-10 h-8 px-2 bg-background/80 backdrop-blur-sm hover:bg-background",
                          config.variant === "sidecar" && "left-6 right-auto",
                        )}
                      >
                        <RefreshCw className="h-4 w-4 mr-1" />
                        Reset Preview
                      </Button>
                      <iframe
                        key={iframeKey}
                        ref={iframeRef}
                        src="/embed.html"
                        className="h-full w-full border-0"
                        title="Gram Elements Chat"
                      />
                    </div>
                  </div>
                </div>
              </div>

              {/* Installation Mode */}
              <div
                className={`${rightPanelTab === "install" ? "block" : "hidden"}`}
              >
                <InstallationGuide
                  config={config}
                  projectSlug={projectSlug ?? "your-project"}
                />
              </div>
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function InstallationGuide({
  config,
  projectSlug,
}: {
  config: ElementsFormConfig;
  projectSlug: string;
}) {
  const [currentStep, setCurrentStep] = useState(1);
  const [selectedProduct, setSelectedProduct] = useState<string | null>(null);
  const [selectedFramework, setSelectedFramework] = useState<
    "nextjs" | "react" | null
  >(null);
  const [generatedApiKey, setGeneratedApiKey] = useState<string | null>(null);
  const [keyCreationAttempted, setKeyCreationAttempted] = useState(false);

  const { data: existingKeys } = useListAPIKeys(
    {},
    { sessionHeaderGramSession: "" },
  );

  const createApiKeyMutation = useCreateAPIKeyMutation();

  // Create API key when entering step 3
  useEffect(() => {
    if (
      currentStep >= 3 &&
      !generatedApiKey &&
      !keyCreationAttempted &&
      !createApiKeyMutation.isPending &&
      existingKeys
    ) {
      setKeyCreationAttempted(true);

      // Generate a unique name by checking existing keys
      const baseKeyName = `Elements - ${projectSlug}`;
      const existingNames = new Set(
        existingKeys.keys?.map((k) => k.name) ?? [],
      );

      let keyName = baseKeyName;
      let suffix = 1;
      while (existingNames.has(keyName)) {
        keyName = `${baseKeyName} (${suffix})`;
        suffix++;
      }

      createApiKeyMutation.mutate(
        {
          security: { sessionHeaderGramSession: "" },
          request: {
            createKeyForm: {
              name: keyName,
              scopes: ["chat"],
            },
          },
        },
        {
          onSuccess: (data) => {
            if (data.key) {
              setGeneratedApiKey(data.key);
            }
          },
        },
      );
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentStep, existingKeys]);

  const mcpUrl = config.mcp || `https://app.getgram.ai/mcp/${projectSlug}`;

  const getPeerDeps = () => {
    const pm = selectedFramework === "nextjs" ? "npm" : "pnpm";
    return `${pm} add react react-dom @assistant-ui/react @assistant-ui/react-markdown motion remark-gfm zustand vega shiki`;
  };

  const getElementsInstall = () => {
    const pm = selectedFramework === "nextjs" ? "npm" : "pnpm";
    return `${pm} add @gram-ai/elements`;
  };

  const getEnvContent = () => {
    const apiKey = generatedApiKey || "your_api_key_here";
    return `GRAM_API_KEY=${apiKey}
EMBED_ORIGIN=http://localhost:3000 # Replace with your actual origin`;
  };

  const getNextjsApiRoute = () => {
    return `// pages/api/session.ts
import type { NextApiRequest, NextApiResponse } from "next";
import { createElementsServerHandlers } from "@gram-ai/elements/server";

// Disable Next.js body parsing so the handler can read the raw stream.
export const config = {
  api: {
    bodyParser: false,
  },
};

const handlers = createElementsServerHandlers();

export default async function handler(
  req: NextApiRequest,
  res: NextApiResponse
) {
  await handlers.session(req, res, {
    userIdentifier: "user-123", // Replace with actual user ID
    embedOrigin: process.env.EMBED_ORIGIN || "http://localhost:3000",
  });
}`;
  };

  const getViteApiRoute = () => {
    return `// server.ts (Express)
import express from "express";
import { createElementsServerHandlers } from "@gram-ai/elements/server";

const app = express();
const handlers = createElementsServerHandlers();

app.use(express.json());

app.post("/chat/session", (req, res) =>
  handlers.session(req, res, {
    // Replace with your actual origin
    embedOrigin: process.env.EMBED_ORIGIN || "http://localhost:3000",
    userIdentifier: "user-123", // Replace with actual user ID
    expiresAfter: 3600,
  })
);

app.listen(3001, () => {
  console.log("Server running on http://localhost:3001");
});`;
  };

  const getComponentCode = () => {
    const isNextjs = selectedFramework === "nextjs";
    const useClientDirective = isNextjs ? `"use client";\n\n` : "";
    const sessionEndpoint = isNextjs
      ? "/api/session"
      : "http://localhost:3001/chat/session";

    // Build config options - only include non-default values
    const configLines: string[] = [];
    configLines.push(`  projectSlug: "${projectSlug}",`);
    configLines.push(`  mcp: "${mcpUrl}",`);

    if (config.variant !== "standalone") {
      configLines.push(`  variant: "${config.variant}",`);
    }

    if (config.colorScheme !== "system") {
      configLines.push(`  colorScheme: "${config.colorScheme}",`);
    }

    if (config.density !== "normal") {
      configLines.push(`  density: "${config.density}",`);
    }

    if (config.radius !== "soft") {
      configLines.push(`  radius: "${config.radius}",`);
    }

    // Welcome config
    const welcomeParts: string[] = [];
    if (config.welcomeTitle && config.welcomeTitle !== "Welcome") {
      welcomeParts.push(`    title: "${config.welcomeTitle}",`);
    }
    if (
      config.welcomeSubtitle &&
      config.welcomeSubtitle !== "How can I help you today?"
    ) {
      welcomeParts.push(`    subtitle: "${config.welcomeSubtitle}",`);
    }
    if (welcomeParts.length > 0) {
      configLines.push(`  welcome: {\n${welcomeParts.join("\n")}\n  },`);
    }

    // Composer config
    if (
      config.composerPlaceholder &&
      config.composerPlaceholder !== "Send a message..."
    ) {
      configLines.push(
        `  composer: {\n    placeholder: "${config.composerPlaceholder}",\n  },`,
      );
    }

    // Model config
    if (config.showModelPicker) {
      configLines.push(`  model: {\n    showModelPicker: true,\n  },`);
    }

    // System prompt
    if (config.systemPrompt) {
      const escapedPrompt = config.systemPrompt
        .replace(/\\/g, "\\\\")
        .replace(/"/g, '\\"')
        .replace(/\n/g, "\\n");
      configLines.push(`  systemPrompt: "${escapedPrompt}",`);
    }

    // Modal config (only for widget variant)
    if (config.variant === "widget") {
      const modalParts: string[] = [];
      if (config.modalTitle && config.modalTitle !== "Chat") {
        modalParts.push(`    title: "${config.modalTitle}",`);
      }
      if (config.modalPosition !== "bottom-right") {
        modalParts.push(`    position: "${config.modalPosition}",`);
      }
      if (config.modalDefaultOpen) {
        modalParts.push(`    defaultOpen: true,`);
      }
      if (modalParts.length > 0) {
        configLines.push(`  modal: {\n${modalParts.join("\n")}\n  },`);
      }
    }

    // Tools config
    if (config.expandToolGroupsByDefault) {
      configLines.push(
        `  tools: {\n    expandToolGroupsByDefault: true,\n  },`,
      );
    }

    // Add the api.sessionFn config
    configLines.push(`  api: {\n    sessionFn: getSession,\n  },`);

    return `${useClientDirective}import { Chat, ElementsConfig, GramElementsProvider } from "@gram-ai/elements";
import "@gram-ai/elements/elements.css";

// Custom session function for non-standard session endpoint
const getSession = async () => {
  return fetch("${sessionEndpoint}", {
    method: "POST",
    headers: { "Gram-Project": "${projectSlug}" },
  })
    .then((res) => res.json())
    .then((data) => data.client_token);
};

const config: ElementsConfig = {
${configLines.join("\n")}
};

export default function GramChat() {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  );
}`;
  };

  const products = [
    {
      id: "chat",
      name: "AI Chat",
      description: "Embed AI assistants",
      preview: ChatPreview,
      available: true,
    },
    {
      id: "search",
      name: "AI Search",
      description: "Semantic search",
      preview: SearchPreview,
      available: false,
    },
    {
      id: "notifications",
      name: "Notifications",
      description: "Notify your users",
      preview: NotificationsPreview,
      available: false,
    },
    {
      id: "docs",
      name: "Documentation",
      description: "Auto-generated docs",
      preview: DocsPreview,
      available: false,
    },
  ];

  const frameworks = [
    {
      id: "nextjs" as const,
      name: "Next.js",
      icon: NextJsIcon,
    },
    {
      id: "react" as const,
      name: "React + Express",
      icon: ReactIcon,
    },
  ];

  const handleProductSelect = (productId: string) => {
    if (products.find((p) => p.id === productId)?.available) {
      setSelectedProduct(productId);
      setCurrentStep(2);
    }
  };

  const handleFrameworkSelect = (frameworkId: "nextjs" | "react" | null) => {
    setSelectedFramework(frameworkId);
    setCurrentStep(3);
  };

  return (
    <div className="relative pl-10">
      {/* Vertical line */}
      <div className="absolute left-[11px] top-6 bottom-0 w-px bg-border" />

      {/* Step 1: What are you building? */}
      <WizardStep
        number={1}
        title="What are you building?"
        description="Choose from common AI experiences or go fully custom."
        isActive={currentStep >= 1}
        isCompleted={currentStep > 1}
        completedSummary={
          selectedProduct
            ? products.find((p) => p.id === selectedProduct)?.name
            : undefined
        }
        onEdit={() => setCurrentStep(1)}
      >
        <div className="grid grid-cols-2 lg:grid-cols-3 gap-4 mt-6">
          {products.map((product) => (
            <button
              key={product.id}
              onClick={() => handleProductSelect(product.id)}
              disabled={!product.available}
              className={`group relative flex flex-col rounded-lg border text-left transition-all overflow-hidden bg-background ${
                selectedProduct === product.id && product.available
                  ? "border-primary ring-2 ring-primary"
                  : product.available
                    ? "border-border hover:border-muted-foreground/50 hover:shadow-sm"
                    : "border-border/50 cursor-not-allowed"
              } ${!product.available ? "opacity-50" : ""}`}
            >
              {/* Preview area */}
              <div
                className={`h-36 w-full p-3 overflow-hidden transition-all duration-200 ${
                  selectedProduct === product.id
                    ? ""
                    : "grayscale group-hover:grayscale-0"
                }`}
                style={{
                  background:
                    selectedProduct === product.id
                      ? "linear-gradient(135deg, #89CFF0 0%, #5DADE2 25%, #3498DB 50%, #85C1E9 75%, #AED6F1 100%)"
                      : "hsl(var(--muted) / 0.3)",
                }}
              >
                <product.preview />
              </div>
              {/* Content */}
              <div className="p-4 pt-3 border-t">
                <div className="flex items-start justify-between">
                  <div>
                    <span className="font-medium text-sm block">
                      {product.name}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {product.description}
                    </span>
                  </div>
                  {selectedProduct === product.id && product.available && (
                    <Check className="h-5 w-5 text-primary shrink-0" />
                  )}
                </div>
              </div>
              {!product.available && (
                <div className="absolute inset-0 flex items-center justify-center bg-background/70 backdrop-blur-[1px]">
                  <span className="text-[10px] font-semibold text-muted-foreground bg-muted px-2.5 py-1 rounded-full uppercase tracking-wide">
                    Coming Soon
                  </span>
                </div>
              )}
            </button>
          ))}
        </div>
      </WizardStep>

      {/* Step 2: Select your technology */}
      <WizardStep
        number={2}
        title="Select your technology"
        description="Choose one of the following step-by-step quickstart guides to help you get started."
        isActive={currentStep >= 2}
        isCompleted={currentStep > 2}
        completedSummary={
          selectedFramework
            ? frameworks.find((f) => f.id === selectedFramework)?.name
            : undefined
        }
        onEdit={() => setCurrentStep(2)}
      >
        {currentStep >= 2 && (
          <div className="flex gap-4 mt-6">
            {frameworks.map((fw) => (
              <button
                key={fw.id}
                onClick={() => handleFrameworkSelect(fw.id)}
                className={`relative flex flex-col items-start p-4 rounded-lg border text-left transition-all min-w-[140px] ${
                  selectedFramework === fw.id
                    ? "border-primary ring-2 ring-primary bg-primary/5"
                    : "border-border hover:border-muted-foreground/50"
                }`}
              >
                <fw.icon className="h-6 w-6 mb-3" />
                <span className="font-medium text-sm flex items-center gap-1.5">
                  {fw.name}
                  <ArrowRight className="h-3.5 w-3.5 text-muted-foreground" />
                </span>
                <span className="text-[10px] font-semibold tracking-wide text-amber-700 dark:text-amber-400 uppercase mt-2">
                  AI Chat
                </span>
              </button>
            ))}
          </div>
        )}
      </WizardStep>

      {/* Step 3: Install & configure */}
      <WizardStep
        number={3}
        title="Install & configure"
        isActive={currentStep >= 3}
        isCompleted={false}
      >
        {currentStep >= 3 && selectedFramework && (
          <div className="mt-6 space-y-6">
            {/* Step 3a: Install */}
            <SetupStep
              number="a"
              title="Install packages"
              description="Run these commands in your terminal"
            >
              <div className="space-y-2">
                <CodeBlock code={getPeerDeps()} language="bash">
                  <CodeBlockCopyButton />
                </CodeBlock>
                <CodeBlock code={getElementsInstall()} language="bash">
                  <CodeBlockCopyButton />
                </CodeBlock>
              </div>
            </SetupStep>

            {/* Step 3b: Environment */}
            <SetupStep
              number="b"
              title="Add environment variables"
              description={
                <>
                  Add to{" "}
                  <code className="text-xs bg-muted px-1.5 py-0.5 rounded font-mono">
                    .env.local
                  </code>
                </>
              }
            >
              <CodeBlock code={getEnvContent()} language="bash">
                <CodeBlockCopyButton />
              </CodeBlock>
            </SetupStep>

            {/* Step 3c: API Route */}
            <SetupStep
              number="c"
              title={
                selectedFramework === "nextjs"
                  ? "Create session API route"
                  : "Create session endpoint"
              }
              description={
                selectedFramework === "nextjs" ? (
                  <>
                    Create{" "}
                    <code className="text-xs bg-muted px-1.5 py-0.5 rounded font-mono">
                      pages/api/session.ts
                    </code>
                  </>
                ) : (
                  <>
                    Create{" "}
                    <code className="text-xs bg-muted px-1.5 py-0.5 rounded font-mono">
                      server.ts
                    </code>
                  </>
                )
              }
            >
              <CodeBlock
                code={
                  selectedFramework === "nextjs"
                    ? getNextjsApiRoute()
                    : getViteApiRoute()
                }
                language="typescript"
                className="max-h-[300px] overflow-y-auto"
              >
                <CodeBlockCopyButton />
              </CodeBlock>
            </SetupStep>

            {/* Step 3d: Component */}
            <SetupStep
              number="d"
              title="Add the chat component"
              description={
                selectedFramework === "nextjs" ? (
                  <>
                    Update{" "}
                    <code className="text-xs bg-muted px-1.5 py-0.5 rounded font-mono">
                      app/page.tsx
                    </code>
                  </>
                ) : (
                  <>
                    Update{" "}
                    <code className="text-xs bg-muted px-1.5 py-0.5 rounded font-mono">
                      src/App.tsx
                    </code>
                  </>
                )
              }
            >
              <CodeBlock
                code={getComponentCode()}
                language="typescript"
                className="max-h-[300px] overflow-y-auto"
              >
                <CodeBlockCopyButton />
              </CodeBlock>
            </SetupStep>
          </div>
        )}
      </WizardStep>

      {/* Step 4: You're all set */}
      <WizardStep
        number={4}
        title="You're all set!"
        description="Run your app and the chat widget will appear."
        isActive={currentStep >= 3}
        isCompleted={false}
        isLast
      >
        {currentStep >= 3 && (
          <div className="mt-6 space-y-6">
            <NextStep
              letter="a"
              title="Read the documentation"
              description="Learn about all configuration options"
            >
              <a
                href="https://github.com/speakeasy-api/gram/blob/main/elements/docs/_media/ElementsConfig.md"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-3 p-4 rounded-lg border bg-card hover:bg-muted/50 transition-colors group"
              >
                <div className="w-10 h-10 rounded-lg bg-[#24292f] dark:bg-[#f0f6fc] flex items-center justify-center shrink-0">
                  <svg
                    className="w-6 h-6 text-white dark:text-[#24292f]"
                    viewBox="0 0 24 24"
                    fill="currentColor"
                  >
                    <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
                  </svg>
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium group-hover:text-primary transition-colors">
                    ElementsConfig.md
                  </p>
                  <p className="text-xs text-muted-foreground">
                    speakeasy-api/gram
                  </p>
                </div>
                <ArrowRight className="w-4 h-4 text-muted-foreground group-hover:text-primary transition-colors" />
              </a>
            </NextStep>

            <NextStep
              letter="b"
              title="Need help?"
              description="Run into any issues? We're here to help"
            >
              <a
                href="https://calendly.com/d/ctgg-5dv-3kw/intro-to-gram-call"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-4 p-4 rounded-lg border bg-card hover:bg-muted/50 transition-colors group"
              >
                <div className="w-12 h-12 rounded-full bg-linear-to-br from-blue-500 to-blue-600 flex items-center justify-center shrink-0">
                  <span className="text-lg font-semibold text-white">S</span>
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium group-hover:text-primary transition-colors">
                    Book a call with us
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Schedule time to get help setting up
                  </p>
                </div>
                <ArrowRight className="w-4 h-4 text-muted-foreground group-hover:text-primary transition-colors" />
              </a>
            </NextStep>
          </div>
        )}
      </WizardStep>
    </div>
  );
}

function SetupStep({
  number,
  title,
  description,
  children,
}: {
  number: string;
  title: string;
  description: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex gap-4">
      <div className="shrink-0 w-7 h-7 rounded-full bg-muted flex items-center justify-center">
        <span className="text-xs font-semibold text-muted-foreground">
          {number}
        </span>
      </div>
      <div className="flex-1 min-w-0 space-y-3">
        <div>
          <h4 className="text-sm font-semibold">{title}</h4>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
        <div>{children}</div>
      </div>
    </div>
  );
}

function NextStep({
  letter,
  title,
  description,
  children,
}: {
  letter: string;
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex gap-4">
      <div className="shrink-0 w-7 h-7 rounded-full bg-muted flex items-center justify-center">
        <span className="text-xs font-semibold text-muted-foreground">
          {letter}
        </span>
      </div>
      <div className="flex-1 min-w-0 space-y-3">
        <div>
          <h4 className="text-sm font-semibold">{title}</h4>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
        <div>{children}</div>
      </div>
    </div>
  );
}

function WizardStep({
  number,
  title,
  description,
  isActive,
  isCompleted,
  isLast,
  completedSummary,
  onEdit,
  children,
}: {
  number: number;
  title: string;
  description?: string;
  isActive: boolean;
  isCompleted: boolean;
  isLast?: boolean;
  completedSummary?: string;
  onEdit?: () => void;
  children?: React.ReactNode;
}) {
  return (
    <div
      className={`relative pb-10 ${isLast ? "pb-0" : ""} ${!isActive ? "opacity-40 pointer-events-none" : ""}`}
    >
      {/* Step indicator - positioned to the left of the content */}
      <div
        className={`absolute -left-10 top-0 w-6 h-6 rounded-full flex items-center justify-center bg-background border ${
          isCompleted
            ? "border-primary bg-primary text-primary-foreground"
            : isActive
              ? "border-muted-foreground"
              : "border-border"
        }`}
      >
        {isCompleted ? (
          <Check className="h-3.5 w-3.5" />
        ) : (
          <span
            className={`text-xs font-medium ${isActive ? "text-foreground" : "text-muted-foreground"}`}
          >
            {number}
          </span>
        )}
      </div>

      <div>
        {isCompleted && completedSummary ? (
          // Collapsed completed state
          <div className="flex items-center gap-2">
            <span className="font-semibold text-[15px] text-muted-foreground">
              {title}
            </span>
            <span className="text-[15px] text-foreground">
              {completedSummary}
            </span>
            {onEdit && (
              <button
                onClick={onEdit}
                className="ml-1 p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                title="Change selection"
              >
                <Pencil className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        ) : (
          // Active/pending state
          <>
            <h3 className="font-semibold text-[15px] leading-6">{title}</h3>
            {description && !isCompleted && (
              <p className="text-sm text-muted-foreground mt-1">
                {description}
              </p>
            )}
            {children}
          </>
        )}
      </div>
    </div>
  );
}

// Product Preview Components
function ChatPreview() {
  return (
    <div className="w-full h-full bg-white rounded-md border shadow-sm flex flex-col text-[10px]">
      {/* Header */}
      <div className="px-2 py-1.5 border-b flex items-center gap-1.5">
        <div className="w-2 h-2 rounded-full bg-blue-500" />
        <span className="font-medium text-[9px] text-slate-600">
          AI Assistant
        </span>
      </div>
      {/* Messages */}
      <div className="flex-1 p-2 space-y-1.5 overflow-hidden">
        <div className="flex gap-1.5">
          <div className="w-4 h-4 rounded-full bg-linear-to-br from-blue-400 to-purple-500 shrink-0" />
          <div className="bg-slate-100 rounded px-1.5 py-1 max-w-[80%]">
            <div className="w-16 h-1.5 bg-slate-300 rounded" />
          </div>
        </div>
        <div className="flex gap-1.5 justify-end">
          <div className="bg-blue-500 rounded px-1.5 py-1 max-w-[80%]">
            <div className="w-12 h-1.5 bg-blue-300 rounded" />
          </div>
        </div>
        <div className="flex gap-1.5">
          <div className="w-4 h-4 rounded-full bg-linear-to-br from-blue-400 to-purple-500 shrink-0" />
          <div className="bg-slate-100 rounded px-1.5 py-1 max-w-[80%]">
            <div className="w-20 h-1.5 bg-slate-300 rounded mb-1" />
            <div className="w-14 h-1.5 bg-slate-300 rounded" />
          </div>
        </div>
      </div>
      {/* Input */}
      <div className="px-2 py-1.5 border-t">
        <div className="bg-slate-100 rounded-full px-2 py-1 flex items-center">
          <div className="w-10 h-1.5 bg-slate-300 rounded" />
        </div>
      </div>
    </div>
  );
}

function SearchPreview() {
  return (
    <div className="w-full h-full bg-white rounded-md border shadow-sm flex flex-col text-[10px] p-2">
      {/* Search bar */}
      <div className="bg-slate-100 rounded-md px-2 py-1.5 flex items-center gap-1.5 mb-2">
        <Search className="w-3 h-3 text-violet-500" />
        <div className="w-16 h-1.5 bg-slate-300 rounded" />
      </div>
      {/* Results */}
      <div className="space-y-1.5 flex-1">
        <div className="bg-violet-50 rounded p-1.5 border-l-2 border-violet-400">
          <div className="w-full h-1.5 bg-slate-400 rounded mb-1" />
          <div className="w-3/4 h-1.5 bg-slate-300 rounded" />
        </div>
        <div className="bg-slate-50 rounded p-1.5">
          <div className="w-full h-1.5 bg-slate-300 rounded mb-1" />
          <div className="w-2/3 h-1.5 bg-slate-200 rounded" />
        </div>
        <div className="bg-slate-50 rounded p-1.5">
          <div className="w-3/4 h-1.5 bg-slate-300 rounded mb-1" />
          <div className="w-1/2 h-1.5 bg-slate-200 rounded" />
        </div>
      </div>
    </div>
  );
}

function NotificationsPreview() {
  return (
    <div className="w-full h-full bg-white rounded-md border shadow-sm flex flex-col text-[10px]">
      {/* Header */}
      <div className="px-2 py-1.5 border-b flex items-center justify-between">
        <span className="font-medium text-[9px] text-slate-600">
          Notifications
        </span>
        <div className="text-[8px] text-blue-500">Mark all as read</div>
      </div>
      {/* Notifications */}
      <div className="flex-1 p-1.5 space-y-1">
        <div className="flex gap-1.5 p-1 rounded bg-blue-50">
          <div className="w-4 h-4 rounded-full bg-linear-to-br from-green-400 to-emerald-500 shrink-0" />
          <div className="flex-1">
            <div className="w-full h-1.5 bg-slate-400 rounded mb-1" />
            <div className="w-2/3 h-1.5 bg-slate-300 rounded" />
          </div>
          <div className="w-1.5 h-1.5 rounded-full bg-blue-500 shrink-0" />
        </div>
        <div className="flex gap-1.5 p-1 rounded">
          <div className="w-4 h-4 rounded-full bg-linear-to-br from-orange-400 to-red-500 shrink-0" />
          <div className="flex-1">
            <div className="w-3/4 h-1.5 bg-slate-300 rounded mb-1" />
            <div className="w-1/2 h-1.5 bg-slate-200 rounded" />
          </div>
        </div>
        <div className="flex gap-1.5 p-1 rounded">
          <div className="w-4 h-4 rounded-full bg-linear-to-br from-purple-400 to-pink-500 shrink-0" />
          <div className="flex-1">
            <div className="w-full h-1.5 bg-slate-300 rounded mb-1" />
            <div className="w-3/4 h-1.5 bg-slate-200 rounded" />
          </div>
        </div>
      </div>
    </div>
  );
}

function DocsPreview() {
  return (
    <div className="w-full h-full bg-white rounded-md border shadow-sm flex text-[10px]">
      {/* Sidebar */}
      <div className="w-1/3 border-r p-1.5 space-y-1 bg-slate-50">
        <div className="w-full h-1.5 bg-blue-500 rounded" />
        <div className="w-3/4 h-1.5 bg-blue-300 rounded ml-1.5" />
        <div className="w-2/3 h-1.5 bg-slate-300 rounded ml-1.5" />
        <div className="w-full h-1.5 bg-slate-400 rounded mt-2" />
        <div className="w-3/4 h-1.5 bg-slate-300 rounded ml-1.5" />
      </div>
      {/* Content */}
      <div className="flex-1 p-2 space-y-2">
        <div className="w-1/2 h-2 bg-slate-700 rounded" />
        <div className="space-y-1">
          <div className="w-full h-1.5 bg-slate-300 rounded" />
          <div className="w-full h-1.5 bg-slate-300 rounded" />
          <div className="w-3/4 h-1.5 bg-slate-300 rounded" />
        </div>
        <div className="bg-slate-800 rounded p-1.5 mt-2">
          <div className="w-full h-1.5 bg-emerald-400 rounded" />
          <div className="w-2/3 h-1.5 bg-slate-500 rounded mt-1" />
        </div>
      </div>
    </div>
  );
}

function ReactIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M14.23 12.004a2.236 2.236 0 0 1-2.235 2.236 2.236 2.236 0 0 1-2.236-2.236 2.236 2.236 0 0 1 2.235-2.236 2.236 2.236 0 0 1 2.236 2.236zm2.648-10.69c-1.346 0-3.107.96-4.888 2.622-1.78-1.653-3.542-2.602-4.887-2.602-.41 0-.783.093-1.106.278-1.375.793-1.683 3.264-.973 6.365C1.98 8.917 0 10.42 0 12.004c0 1.59 1.99 3.097 5.043 4.03-.704 3.113-.39 5.588.988 6.38.32.187.69.275 1.102.275 1.345 0 3.107-.96 4.888-2.624 1.78 1.654 3.542 2.603 4.887 2.603.41 0 .783-.09 1.106-.275 1.374-.792 1.683-3.263.973-6.365C22.02 15.096 24 13.59 24 12.004c0-1.59-1.99-3.097-5.043-4.032.704-3.11.39-5.587-.988-6.38-.318-.184-.688-.277-1.092-.278zm-.005 1.09v.006c.225 0 .406.044.558.127.666.382.955 1.835.73 3.704-.054.46-.142.945-.25 1.44-.96-.236-2.006-.417-3.107-.534-.66-.905-1.345-1.727-2.035-2.447 1.592-1.48 3.087-2.292 4.105-2.295zm-9.77.02c1.012 0 2.514.808 4.11 2.28-.686.72-1.37 1.537-2.02 2.442-1.107.117-2.154.298-3.113.538-.112-.49-.195-.964-.254-1.42-.23-1.868.054-3.32.714-3.707.19-.09.4-.127.563-.132zm4.882 3.05c.455.468.91.992 1.36 1.564-.44-.02-.89-.034-1.345-.034-.46 0-.915.01-1.36.034.44-.572.895-1.096 1.345-1.565zM12 8.1c.74 0 1.477.034 2.202.093.406.582.802 1.203 1.183 1.86.372.64.71 1.29 1.018 1.946-.308.655-.646 1.31-1.013 1.95-.38.66-.773 1.288-1.18 1.87-.728.063-1.466.098-2.21.098-.74 0-1.477-.035-2.202-.093-.406-.582-.802-1.204-1.183-1.86-.372-.64-.71-1.29-1.018-1.946.303-.657.646-1.313 1.013-1.954.38-.66.773-1.286 1.18-1.868.728-.064 1.466-.098 2.21-.098zm-3.635.254c-.24.377-.48.763-.704 1.16-.225.39-.435.782-.635 1.174-.265-.656-.49-1.31-.676-1.947.64-.15 1.315-.283 2.015-.386zm7.26 0c.695.103 1.365.23 2.006.387-.18.632-.405 1.282-.66 1.933-.2-.39-.41-.783-.64-1.174-.225-.392-.465-.774-.705-1.146zm3.063.675c.484.15.944.317 1.375.498 1.732.74 2.852 1.708 2.852 2.476-.005.768-1.125 1.74-2.857 2.475-.42.18-.88.342-1.355.493-.28-.958-.646-1.956-1.1-2.98.45-1.017.81-2.01 1.085-2.964zm-13.395.004c.278.96.645 1.957 1.1 2.98-.45 1.017-.812 2.01-1.086 2.964-.484-.15-.944-.318-1.37-.5-1.732-.737-2.852-1.706-2.852-2.474 0-.768 1.12-1.742 2.852-2.476.42-.18.88-.342 1.356-.494zm11.678 4.28c.265.657.49 1.312.676 1.948-.64.157-1.316.29-2.016.39.24-.375.48-.762.705-1.158.225-.39.435-.788.636-1.18zm-9.945.02c.2.392.41.783.64 1.175.23.39.465.772.705 1.143-.695-.102-1.365-.23-2.006-.386.18-.63.406-1.282.66-1.933zM17.92 16.32c.112.493.2.968.254 1.423.23 1.868-.054 3.32-.714 3.708-.147.09-.338.128-.563.128-1.012 0-2.514-.807-4.11-2.28.686-.72 1.37-1.536 2.02-2.44 1.107-.118 2.154-.3 3.113-.54zm-11.83.01c.96.234 2.006.415 3.107.532.66.905 1.345 1.727 2.035 2.446-1.595 1.483-3.092 2.295-4.11 2.295-.22-.005-.406-.05-.553-.132-.666-.38-.955-1.834-.73-3.703.054-.46.142-.944.25-1.438zm4.56.64c.44.02.89.034 1.345.034.46 0 .915-.01 1.36-.034-.44.572-.895 1.095-1.345 1.565-.455-.47-.91-.993-1.36-1.565z" />
    </svg>
  );
}

function NextJsIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor">
      <path d="M11.572 0c-.176 0-.31.001-.358.007a19.76 19.76 0 0 1-.364.033C7.443.346 4.25 2.185 2.228 5.012a11.875 11.875 0 0 0-2.119 5.243c-.096.659-.108.854-.108 1.747s.012 1.089.108 1.748c.652 4.506 3.86 8.292 8.209 9.695.779.251 1.6.422 2.534.525.363.04 1.935.04 2.299 0 1.611-.178 2.977-.577 4.323-1.264.207-.106.247-.134.219-.158-.02-.013-.9-1.193-1.955-2.62l-1.919-2.592-2.404-3.558a338.739 338.739 0 0 0-2.422-3.556c-.009-.002-.018 1.579-.023 3.51-.007 3.38-.01 3.515-.052 3.595a.426.426 0 0 1-.206.214c-.075.037-.14.044-.495.044H7.81l-.108-.068a.438.438 0 0 1-.157-.171l-.05-.106.006-4.703.007-4.705.072-.092a.645.645 0 0 1 .174-.143c.096-.047.134-.051.54-.051.478 0 .558.018.682.154.035.038 1.337 1.999 2.895 4.361a10760.433 10760.433 0 0 0 4.735 7.17l1.9 2.879.096-.063a12.317 12.317 0 0 0 2.466-2.163 11.944 11.944 0 0 0 2.824-6.134c.096-.66.108-.854.108-1.748 0-.893-.012-1.088-.108-1.747-.652-4.506-3.859-8.292-8.208-9.695a12.597 12.597 0 0 0-2.499-.523A33.119 33.119 0 0 0 11.572 0zm4.069 7.217c.347 0 .408.005.486.047a.473.473 0 0 1 .237.277c.018.06.023 1.365.018 4.304l-.006 4.218-.744-1.14-.746-1.14v-3.066c0-1.982.01-3.097.023-3.15a.478.478 0 0 1 .233-.296c.096-.05.13-.054.5-.054z" />
    </svg>
  );
}
