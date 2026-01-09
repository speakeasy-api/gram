import { CodeBlock } from "@/components/code";
import { Expandable } from "@/components/expandable";
import { GramLogo } from "@/components/gram-logo";
import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { OpenApiSourceInput } from "@/components/OpenApiSourceInput";
import { ProjectSelector } from "@/components/project-menu";
import { ToolBadge } from "@/components/tool-badge";
import { ErrorAlert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { SkeletonParagraph } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Type } from "@/components/ui/type";
import { AsciiVideo, useWebGLStore } from "@/components/webgl";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useListTools } from "@/hooks/toolTypes";
import { slugify } from "@/lib/constants";
import { handleAPIError } from "@/lib/errors";
import { filterHttpTools, useGroupedHttpTools } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import {
  invalidateAllLatestDeployment,
  invalidateAllListToolsets,
  invalidateAllToolset,
} from "@gram/client/react-query";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  ChevronRight,
  CircleCheckIcon,
  Database,
  FileCode,
  FileJson2,
  MessageSquare,
  RefreshCcw,
  ServerCog,
  SquareFunction,
  Store,
  Upload,
  Wrench,
  X,
} from "lucide-react";
import { AnimatePresence, motion, useMotionValue } from "motion/react";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import { toast } from "sonner";
import { useMcpSlugValidation } from "../mcp/MCPDetails";
import { DeploymentLogs, useUploadOpenAPISteps } from "./UploadOpenAPI";

type OnboardingPath = "openapi" | "cli";
type OnboardingStep =
  | "initial-choice"
  | "first-party-choice"
  | "upload"
  | "cli-setup"
  | "toolset"
  | "mcp";

export const START_PATH_PARAM = "start-path";
export const START_STEP_PARAM = "start-step";

export function OnboardingWizard() {
  const { orgSlug } = useParams();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const [searchParams] = useSearchParams();

  // Feature flag for Gram functions flow
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  const [selectedPath, setSelectedPath] = useState<OnboardingPath>();
  const [currentStep, setCurrentStep] =
    useState<OnboardingStep>("initial-choice");
  const [toolsetName, setToolsetName] = useState<string>();
  const [mcpSlug, setMcpSlug] = useState<string>();

  const startStep = searchParams.get(START_STEP_PARAM);
  const startPath = searchParams.get(START_PATH_PARAM);

  // If we have a start path and step, set the selected path and step
  useEffect(() => {
    if (startPath && startStep) {
      setSelectedPath(startPath as OnboardingPath);
      setCurrentStep(startStep as OnboardingStep);
    } else if (startStep === "first-party-choice") {
      setCurrentStep("first-party-choice");
    }
  }, [startPath, startStep]);

  // Initialize mcpSlug when toolsetName changes
  useEffect(() => {
    if (toolsetName && !mcpSlug) {
      setMcpSlug(`${orgSlug}-${toolsetName}`);
    }
  }, [toolsetName, mcpSlug, orgSlug]);

  return (
    <Stack direction={"horizontal"} className="h-[100vh] w-full">
      <div className="w-1/2 h-full border-r-1 ">
        <LHS
          currentStep={currentStep}
          setCurrentStep={setCurrentStep}
          selectedPath={selectedPath}
          setSelectedPath={setSelectedPath}
          toolsetName={toolsetName}
          setToolsetName={setToolsetName}
          mcpSlug={mcpSlug}
          setMcpSlug={setMcpSlug}
          isFunctionsEnabled={isFunctionsEnabled}
          routes={routes}
        />
      </div>
      <div className="w-1/2 h-full bg-background overflow-hidden">
        <AnimatedRightSide
          currentStep={currentStep}
          toolsetName={toolsetName}
          mcpSlug={mcpSlug}
        />
      </div>
    </Stack>
  );
}

const Step = ({
  text,
  icon,
  active,
  completed,
}: {
  text: string;
  icon: React.ReactNode;
  active?: boolean;
  completed?: boolean;
}) => {
  return (
    <Stack direction={"horizontal"} gap={2} align={"center"}>
      <span
        className={cn(
          "rounded-lg bg-muted h-8 w-8 flex items-center justify-center border border-border",
          completed &&
            "bg-success text-success-foreground border-success-softest",
          !active && !completed && "border-neutral-softest",
        )}
      >
        {completed ? <Check className="w-4 h-4" /> : icon}
      </span>
      <span className={cn(!active && "text-muted-foreground", "text-body-sm")}>
        {text}
      </span>
    </Stack>
  );
};

export const ChoiceCard = ({
  onClick,
  icon: Icon,
  title,
  description,
}: {
  onClick: () => void;
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  title: string;
  description: string;
}) => {
  return (
    <button
      onClick={onClick}
      className="p-8 bg-secondary rounded-lg hover:bg-accent transition-colors text-left group flex flex-col items-start relative shadow-[inset_0px_1px_1px_0px_rgba(255,255,255,0.24),inset_0px_-1px_1px_0px_rgba(0,0,0,0.08)]"
    >
      <Icon className="w-8 h-8 text-primary mb-3 shrink-0" strokeWidth={1.5} />
      <div className="flex flex-col gap-1">
        <Type className="text-heading-sm">{title}</Type>
        <Type small className="text-muted">
          {description}
        </Type>
      </div>
    </button>
  );
};

type RoutesWithGoTo = ReturnType<typeof useRoutes>;

const LHS = ({
  currentStep,
  setCurrentStep,
  selectedPath,
  setSelectedPath,
  toolsetName,
  setToolsetName,
  mcpSlug,
  setMcpSlug,
  isFunctionsEnabled,
  routes,
}: {
  currentStep: OnboardingStep;
  setCurrentStep: (step: OnboardingStep) => void;
  selectedPath: OnboardingPath | undefined;
  setSelectedPath: (path: OnboardingPath) => void;
  toolsetName: string | undefined;
  setToolsetName: (name: string) => void;
  mcpSlug: string | undefined;
  setMcpSlug: (slug: string) => void;
  isFunctionsEnabled: boolean;
  routes: RoutesWithGoTo;
}) => {
  const [createdToolset, setCreatedToolset] = useState<Toolset>();
  const { organization } = useSession();
  const [showTopBlur, setShowTopBlur] = useState(false);
  const [showBottomBlur, setShowBottomBlur] = useState(false);
  const contentScrollRef = useRef<HTMLDivElement>(null);
  const telemetry = useTelemetry();

  // Check scroll position to show/hide blur gradients
  const handleScroll = () => {
    const element = contentScrollRef.current;
    if (!element) return;

    const { scrollTop, scrollHeight, clientHeight } = element;

    // Show top blur if scrolled down from top
    setShowTopBlur(scrollTop > 10);

    // Show bottom blur if not at bottom
    setShowBottomBlur(scrollTop + clientHeight < scrollHeight - 10);
  };

  // Update blur visibility on content changes
  useEffect(() => {
    handleScroll();
  }, [currentStep]);

  const isNotChoiceStep =
    currentStep !== "first-party-choice" && currentStep !== "initial-choice";

  const lowerLeft =
    organization?.projects.length > 1 ? (
      <div className="max-w-sm">
        <ProjectSelector />
      </div>
    ) : (
      <span className="text-body-sm text-muted-foreground">
        © 2025 Speakeasy
      </span>
    );

  return (
    <div className="h-full flex flex-col relative bg-card">
      {/* Fixed Header */}
      <Stack align={"center"}>
        <Stack
          direction={"horizontal"}
          align={"center"}
          justify={"space-between"}
          className="w-full border-b h-16 px-6 mb-8"
        >
          <Link className="hover:bg-accent p-2 rounded-md" to="/">
            <GramLogo className="w-25" />
          </Link>
          <a href="https://docs.getgram.ai/" target="_blank">
            <Type mono className="text-[15px] font-normal">
              VIEW DOCS
            </Type>
          </a>
        </Stack>
        {isNotChoiceStep && (
          <Stack direction={"horizontal"} gap={6} align={"center"}>
            <Step
              text={selectedPath === "cli" ? "Setup CLI" : "Upload OpenAPI"}
              icon={<Upload className="w-4 h-4" />}
              active={currentStep === "upload" || currentStep === "cli-setup"}
              completed={currentStep === "toolset" || currentStep === "mcp"}
            />
            <ChevronRight className="w-4 h-4 text-muted-foreground" />
            <Step
              text="Create Toolset"
              icon={<Wrench className="w-4 h-4" />}
              active={currentStep === "toolset"}
              completed={currentStep === "mcp"}
            />
            <ChevronRight className="w-4 h-4 text-muted-foreground" />
            <Step
              text="Configure MCP"
              icon={<ServerCog className="w-4 h-4" />}
              active={currentStep === "mcp"}
            />
          </Stack>
        )}
      </Stack>

      {/* Content - absolutely positioned within left container */}
      <div
        className="absolute inset-x-0 bottom-16 pointer-events-none"
        style={{
          top: isNotChoiceStep ? "160px" : "64px",
        }}
      >
        {/* Blur gradient at top (when scrolled) */}
        {showTopBlur && (
          <div className="absolute inset-x-0 top-0 h-16 bg-linear-to-b from-card to-transparent pointer-events-none z-20" />
        )}

        {/* Scrollable content */}
        <div
          ref={contentScrollRef}
          onScroll={handleScroll}
          className="h-full overflow-y-auto px-16 flex items-center justify-center"
        >
          <Stack className="w-full max-w-3xl gap-8 pointer-events-auto z-10 my-auto">
            {currentStep === "initial-choice" && (
              <InitialChoiceStep
                setCurrentStep={setCurrentStep}
                setSelectedPath={setSelectedPath}
                routes={routes}
                isFunctionsEnabled={isFunctionsEnabled}
                onChoiceSelected={(choice) => {
                  telemetry.capture("onboarding_choice_selected", { choice });
                }}
              />
            )}
            {currentStep === "first-party-choice" && (
              <ChoiceStep
                setCurrentStep={setCurrentStep}
                setSelectedPath={setSelectedPath}
                isFunctionsEnabled={isFunctionsEnabled}
              />
            )}
            {currentStep === "upload" && (
              <UploadStep
                setCurrentStep={setCurrentStep}
                setToolsetName={setToolsetName}
              />
            )}
            {currentStep === "cli-setup" && (
              <CliSetupStep setCurrentStep={setCurrentStep} />
            )}
            {currentStep === "toolset" && (
              <ToolsetStep
                toolsetName={toolsetName}
                setToolsetName={setToolsetName}
                setCreatedToolset={setCreatedToolset}
                setCurrentStep={setCurrentStep}
              />
            )}
            {currentStep === "mcp" && (
              <McpStep
                createdToolset={createdToolset}
                mcpSlug={mcpSlug}
                setMcpSlug={setMcpSlug}
              />
            )}
          </Stack>
        </div>

        {/* Blur gradient at bottom (when not at bottom) */}
        {showBottomBlur && (
          <div className="absolute inset-x-0 bottom-0 h-16 bg-linear-to-t from-card to-transparent pointer-events-none z-20" />
        )}
      </div>

      {/* Footer - pinned to bottom */}
      <Stack
        direction={"horizontal"}
        justify={"space-between"}
        align={"center"}
        className="px-6 h-16 mt-auto"
      >
        {lowerLeft}
        <a href="https://x.com/speakeasydev" target="_blank">
          <TwitterIcon className="w-4 h-4 fill-muted-foreground" />
        </a>
      </Stack>
    </div>
  );
};

export const InitialChoiceStep = ({
  setCurrentStep,
  setSelectedPath,
  routes,
  isFunctionsEnabled,
  onChoiceSelected,
}: {
  setCurrentStep?: (step: OnboardingStep) => void;
  setSelectedPath?: (path: OnboardingPath) => void;
  routes: RoutesWithGoTo;
  isFunctionsEnabled: boolean;
  onChoiceSelected: (choice: string) => void;
}) => {
  const navigate = useNavigate();

  const handleConnectToData = () => {
    onChoiceSelected("connect_to_data");
    // If we have step setters (wizard context), update wizard state
    // Otherwise navigate directly to onboarding page with params
    if (setCurrentStep && setSelectedPath) {
      if (isFunctionsEnabled) {
        setCurrentStep("first-party-choice");
      } else {
        setSelectedPath("openapi");
        setCurrentStep("upload");
      }
    } else {
      // Navigate to onboarding with appropriate start params
      navigate(
        routes.onboarding.href() + `?${START_STEP_PARAM}=first-party-choice`,
      );
    }
  };

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Get Started with Gram</span>
        <span className="text-body-sm">What would you like to do?</span>
      </Stack>
      <div className="grid grid-cols-1 gap-4">
        <ChoiceCard
          onClick={() => {
            onChoiceSelected("connect_to_popular_mcps");
            routes.catalog.goTo();
          }}
          icon={Store}
          title="Connect to Popular MCPs"
          description="Browse and connect to official and community-maintained MCP servers"
        />
        <ChoiceCard
          onClick={handleConnectToData}
          icon={Database}
          title="Connect to Your Data"
          description="Create tools and MCPs from your own APIs or deploy custom code"
        />
        <ChoiceCard
          onClick={() => {
            onChoiceSelected("deploy_data_integrated_chat");
            routes.deployChat.goTo();
          }}
          icon={MessageSquare}
          title="Deploy Chat Connected To Your Data"
          description="Build embeddable chat experiences powered by your data"
        />
      </div>
    </>
  );
};

const ChoiceStep = ({
  setCurrentStep,
  setSelectedPath,
  isFunctionsEnabled,
}: {
  setCurrentStep: (step: OnboardingStep) => void;
  setSelectedPath: (path: OnboardingPath) => void;
  isFunctionsEnabled: boolean;
}) => {
  const handleChoice = (path: OnboardingPath) => {
    setSelectedPath(path);
    setCurrentStep(path === "openapi" ? "upload" : "cli-setup");
  };

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Connect to Your Data</span>
        <span className="text-body-sm">
          Choose how you want to create your tools
        </span>
      </Stack>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <ChoiceCard
          onClick={() => handleChoice("openapi")}
          icon={FileCode}
          title="Start from API"
          description="Generate tools from your OpenAPI specification"
        />
        {isFunctionsEnabled && (
          <ChoiceCard
            onClick={() => handleChoice("cli")}
            icon={SquareFunction}
            title="Start from Code"
            description="Deploy custom functions using the Gram CLI"
          />
        )}
      </div>
    </>
  );
};

type LogEntry = {
  id: string;
  message: string;
  type: "info" | "success";
  loading?: boolean;
};

const DEMO_LOGS: LogEntry[] = [
  { id: "1", message: "$ npm create @gram-ai/function@latest", type: "info" },
  { id: "2", message: "✓ Project created in ./my-functions", type: "success" },
  { id: "3", message: "$ cd my-functions", type: "info" },
  {
    id: "4",
    message: "$ brew install speakeasy-api/homebrew-tap/gram",
    type: "info",
  },
  { id: "5", message: "✓ CLI installed", type: "success" },
  { id: "6", message: "$ gram auth", type: "info" },
  { id: "7", message: "✓ Authentication successful", type: "success" },
  { id: "8", message: "$ open .", type: "info" },
  { id: "9", message: "$ npm run build", type: "info" },
  { id: "10", message: "✓ dist/functions.zip", type: "success" },
  {
    id: "11",
    message:
      '$ gram stage function --location dist/functions.zip --name "My Functions" --slug my-functions',
    type: "info",
  },
  { id: "12", message: "✓ Functions staged", type: "success" },
  { id: "13", message: "$ gram push", type: "info" },
  {
    id: "14",
    message: "⏳ Pushing functions...",
    type: "info",
    loading: true,
  },
];

const CliSetupStep = ({
  setCurrentStep,
}: {
  setCurrentStep: (step: OnboardingStep) => void;
}) => {
  const [installMethod, setInstallMethod] = useState<"npm" | "pnpm">("npm");
  const client = useSdkClient();

  const installCommandPrefix = installMethod === "npm" ? "npm run" : "pnpm";

  // We explicitly don't poll to advance this step because the expected flow is that the CLI opens a new window with the next step.

  const commands = [
    {
      label: (
        <>
          Create a new function project. See gram functions{" "}
          <a
            href="https://www.speakeasy.com/docs/gram/getting-started/typescript"
            target="_blank"
            className="text-primary underline cursor-pointer"
          >
            docs
          </a>{" "}
          for more info
        </>
      ),
      command: `${installMethod} create @gram-ai/function@latest`,
      showToggle: true,
    },
    {
      label: "Build your functions",
      command: `${installCommandPrefix} build`,
    },
    {
      label: "Push your functions to Gram",
      command: `${installCommandPrefix} push`,
    },
  ];

  const handleContinue = async () => {
    const tools = await client.tools.list();
    if (tools.tools.length > 0) {
      setCurrentStep("toolset");
    } else {
      toast.error("No tools found. Please retry the build command.");
    }
  };

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Get Started with Gram Functions</span>
        <span className="text-body-sm">
          Run these commands in your terminal
        </span>
      </Stack>

      <Stack gap={4}>
        {commands.map((item, index) => (
          <Stack key={index} gap={2}>
            <Stack
              direction="horizontal"
              justify="space-between"
              align="center"
            >
              <Type small className="font-medium">
                {index + 1}. {item.label}
              </Type>
              {item.showToggle && (
                <Stack direction="horizontal" gap={1}>
                  <Button
                    variant={installMethod === "npm" ? "primary" : "tertiary"}
                    size="sm"
                    onClick={() => setInstallMethod("npm")}
                  >
                    npm
                  </Button>
                  <Button
                    variant={installMethod === "pnpm" ? "primary" : "tertiary"}
                    size="sm"
                    onClick={() => setInstallMethod("pnpm")}
                  >
                    pnpm
                  </Button>
                </Stack>
              )}
            </Stack>
            <CodeBlock children={item.command} language="bash" />
          </Stack>
        ))}
      </Stack>

      <span className="text-body-sm">
        The build command should open a new window with the next step. If it
        doesn't, click{" "}
        <span
          onClick={handleContinue}
          className="text-primary underline cursor-pointer"
        >
          here
        </span>{" "}
        to continue.
      </span>
    </>
  );
};

export const UploadedDocument = ({
  file,
  onReset,
  defaultExpanded = false,
}: {
  file: File;
  onReset: () => void;
  defaultExpanded?: boolean;
}) => {
  const [fileText, setFileText] = useState<string>();

  useEffect(() => {
    if (!file) return;
    if (file.size > 10_000) {
      file
        .slice(0, 10_000)
        .text()
        .then((text) => setFileText(text + "\n..."));
    } else {
      file.text().then(setFileText);
    }
  }, [file]);

  return (
    <Expandable defaultExpanded={defaultExpanded}>
      <Expandable.Trigger>
        <Stack direction={"horizontal"} gap={2} align={"center"}>
          <FileJson2 className="w-4 h-4 text-muted-foreground/70" />
          <Type small mono>
            {file.name}
          </Type>
          <Button
            variant="tertiary"
            onClick={onReset}
            className="size-6 opacity-50 hover:opacity-100"
          >
            <Button.Icon>
              <X className="w-4 h-4" />
            </Button.Icon>
          </Button>
        </Stack>
      </Expandable.Trigger>
      <Expandable.Content className="text-xs">
        {fileText?.length ? (
          <pre className="whitespace-pre-wrap break-all">{fileText}</pre>
        ) : (
          <SkeletonParagraph lines={12} />
        )}
      </Expandable.Content>
    </Expandable>
  );
};

const UploadStep = ({
  setCurrentStep,
  setToolsetName,
}: {
  setCurrentStep: (step: "upload" | "toolset") => void;
  setToolsetName: (toolsetName: string) => void;
}) => {
  const {
    file,
    handleSpecUpload,
    handleUrlUpload,
    createDeployment,
    apiName,
    setApiName,
    apiNameError,
    undoSpecUpload,
  } = useUploadOpenAPISteps(false);

  const [deploymentToShowLogsFor, setDeploymentToShowLogsFor] =
    useState<string>();

  const reset = () => {
    setDeploymentToShowLogsFor(undefined);
    undoSpecUpload();
  };

  const content = file ? (
    <Stack gap={4}>
      <Stack gap={1}>
        <Stack direction={"horizontal"} gap={1} align={"center"}>
          <CircleCheckIcon className="w-4 h-4 text-success-foreground" />
          <Type small className="font-normal">
            OpenAPI Document
          </Type>
        </Stack>
        <UploadedDocument file={file} onReset={reset} />
      </Stack>
      {apiName != null ? (
        <InputField
          placeholder="Petstore"
          value={apiName}
          onChange={(e) => setApiName(e.target.value)}
          maxLength={30}
          label="API Name"
          error={apiNameError}
          hint={"Give your API a meaningful name."}
          required
          autoFocus
        />
      ) : (
        <SkeletonParagraph lines={3} />
      )}
    </Stack>
  ) : (
    <OpenApiSourceInput
      onUpload={handleSpecUpload}
      onUrlUpload={handleUrlUpload}
      className="max-w-full"
    />
  );

  const onContinue = async () => {
    setToolsetName(slugify(apiName || "my-toolset"));
    const deployment = await createDeployment(undefined, true);

    if (
      deployment?.openapiv3ToolCount === 0 ||
      deployment?.status === "failed"
    ) {
      setDeploymentToShowLogsFor(deployment?.id);
      toast.error("Unable to create tools from your OpenAPI spec");
      return;
    }

    setCurrentStep("toolset");
  };

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Upload your OpenAPI spec</span>
        <span className="text-body-sm">
          We will use this to create tools for your API
        </span>
      </Stack>
      {content}
      {deploymentToShowLogsFor && (
        <Expandable>
          <Expandable.Trigger>
            <Type small destructive>
              Unable to create tools from your OpenAPI spec
            </Type>
          </Expandable.Trigger>
          <Expandable.Content>
            <DeploymentLogs deploymentId={deploymentToShowLogsFor} onlyErrors />
          </Expandable.Content>
        </Expandable>
      )}
      <ContinueButton
        disabled={!file || !!apiNameError || !!deploymentToShowLogsFor}
        onClick={onContinue}
        inProgressText="Generating tools"
      />
    </>
  );
};

const ToolsetStep = ({
  toolsetName,
  setToolsetName,
  setCreatedToolset,
  setCurrentStep,
}: {
  toolsetName: string | undefined;
  setToolsetName: (toolsetName: string) => void;
  setCreatedToolset: (toolset: Toolset) => void;
  setCurrentStep: (step: "toolset" | "mcp") => void;
}) => {
  const client = useSdkClient();
  const { data: tools, isLoading: toolsLoading } = useListTools();
  const [createError, setCreateError] = useState<string | null>(null);

  const onContinue = async () => {
    setCreateError(null);

    try {
      if (!toolsetName) {
        throw new Error("No toolset name found");
      }
      if (!tools?.tools.length) {
        throw new Error("No tools found");
      }

      const toolset = await client.toolsets.create({
        createToolsetRequestBody: {
          name: toolsetName,
          description: `A toolset created from your OpenAPI document`,
          toolUrns: tools?.tools.map((tool) => tool.toolUrn) ?? [],
        },
      });

      setCreatedToolset(toolset);
      setCurrentStep("mcp");
    } catch (error) {
      const errorMessage =
        error instanceof Error ? error.message : "Failed to create toolset";
      setCreateError(errorMessage);
      handleAPIError(error, "Failed to create toolset");
    }
  };

  const groupedTools = useGroupedHttpTools(filterHttpTools(tools?.tools ?? []));
  const flattened = groupedTools.flatMap((group) => group.tools);
  const toolsToShow = flattened.slice(0, 25);
  const additionalTools = flattened.slice(25);

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Name Your Toolset</span>
        <span className="text-body-sm">
          This toolset will hold the tools you've added to Gram. We'll make the
          tools it contains available in an MCP Server in the next step.
        </span>
      </Stack>
      <InputField
        placeholder="my-toolset"
        value={toolsetName}
        onChange={(e) => setToolsetName(e.target.value)}
        maxLength={30}
        label="Name"
        error={!toolsetName ? "This field is required" : undefined}
        hint={"Don't worry, you can change this later."}
        required
      />
      {createError && (
        <ErrorAlert
          error={createError}
          title="Failed to create toolset"
          onDismiss={() => setCreateError(null)}
        />
      )}
      {toolsLoading ? (
        <Spinner />
      ) : (
        <Stack
          gap={1}
          direction={"horizontal"}
          align={"center"}
          wrap="wrap"
          className="w-full"
        >
          {toolsToShow.map((tool) => (
            <ToolBadge
              key={tool.name}
              variant={"secondary"}
              tool={{ ...tool, name: tool.displayName, type: "http" }}
            />
          ))}
          {additionalTools.length > 0 && (
            <Badge
              variant={"secondary"}
              className="text-muted-foreground"
              tooltip={
                <Stack>
                  {additionalTools.map((tool) => (
                    <span key={tool.name}>{tool.displayName}</span>
                  ))}
                </Stack>
              }
            >
              + {additionalTools.length} more
            </Badge>
          )}
        </Stack>
      )}
      <ContinueButton
        disabled={!toolsetName}
        onClick={onContinue}
        inProgressText="Creating toolset"
      />
    </>
  );
};

const McpStep = ({
  createdToolset,
  mcpSlug,
  setMcpSlug,
}: {
  createdToolset: Toolset | undefined;
  mcpSlug: string | undefined;
  setMcpSlug: (slug: string) => void;
}) => {
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const routes = useRoutes();
  const org = useOrganization();
  const [updateError, setUpdateError] = useState<string | null>(null);

  const slugError = useMcpSlugValidation(mcpSlug);

  const onContinue = async () => {
    setUpdateError(null);

    try {
      if (!createdToolset) {
        throw new Error("No toolset found");
      }

      if (!mcpSlug) {
        throw new Error("No MCP slug set");
      }

      await client.toolsets.updateBySlug({
        slug: createdToolset.slug,
        updateToolsetRequestBody: {
          mcpSlug,
        },
      });

      // We need to invalidate all queries used in the `emptyProjectRedirect` to avoid looping back to onboarding
      await invalidateAllToolset(queryClient);
      await invalidateAllListToolsets(queryClient);
      await invalidateAllLatestDeployment(queryClient);

      toast.success("MCP server created successfully");
      routes.home.goTo();
    } catch (error) {
      const errorMessage =
        error instanceof Error ? error.message : "Failed to setup MCP server";
      setUpdateError(errorMessage);
      handleAPIError(error, "Failed to setup MCP server");
    }
  };

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Configure MCP</span>
        <span className="text-body-sm">
          Set the slug this MCP server will be hosted at. Custom domains can be
          configured later on.
        </span>
      </Stack>
      <AnyField
        id="mcp-slug"
        label="MCP Server Slug"
        hint={"☑︎ This slug is available!"}
        error={slugError}
        render={(extraProps) => (
          <Input
            {...extraProps}
            placeholder="my-mcp"
            value={mcpSlug || createdToolset?.slug || "my-mcp"}
            onChange={setMcpSlug}
            maxLength={40}
            requiredPrefix={org?.slug ? `${org.slug}-` : ""}
          />
        )}
      />
      {updateError && (
        <ErrorAlert
          error={updateError}
          title="Failed to setup MCP server"
          onDismiss={() => setUpdateError(null)}
        />
      )}
      <ContinueButton disabled={!!slugError} onClick={onContinue} />
    </>
  );
};

const ContinueButton = ({
  disabled,
  inProgressText,
  onClick,
}: {
  disabled?: boolean;
  inProgressText?: string;
  onClick: () => Promise<void>;
}) => {
  const [isLoading, setIsLoading] = useState(false);

  return (
    <Button
      variant="brand"
      disabled={disabled || isLoading}
      onClick={async () => {
        setIsLoading(true);
        try {
          await onClick();
        } catch (_error) {
          // Error is already handled by the individual step components
        } finally {
          setIsLoading(false);
        }
      }}
      className="w-full"
    >
      {isLoading && <Spinner />}
      {isLoading && inProgressText ? inProgressText : "Continue"}
    </Button>
  );
};

const AnimatedRightSide = ({
  currentStep,
  toolsetName,
  mcpSlug,
}: {
  currentStep: OnboardingStep;
  toolsetName: string | undefined;
  mcpSlug: string | undefined;
}) => {
  const setCanvasZIndex = useWebGLStore((state) => state.setCanvasZIndex);
  const setShowAsciiStars = useWebGLStore((state) => state.setShowAsciiStars);

  // Set canvas to be visible (but still allow pointer events through)
  useEffect(() => {
    setCanvasZIndex(1);
    setShowAsciiStars(true);
    return () => {
      setCanvasZIndex(-1);
      setShowAsciiStars(false);
    };
  }, [setCanvasZIndex, setShowAsciiStars]);

  return (
    <div className="w-full h-full bg-background flex items-center justify-center relative overflow-hidden">
      {/* ASCII shader decorations in corners */}
      {/* Top right corner */}
      <div className="absolute top-0 right-0 w-[300px] h-[300px] opacity-30 pointer-events-none -z-10">
        <AsciiVideo videoSrc="/webgl/stars.mp4" className="w-full h-full" />
      </div>

      {/* Bottom left corner - flipped both ways */}
      <div className="absolute bottom-0 left-0 w-[300px] h-[300px] opacity-30 pointer-events-none -z-10">
        <AsciiVideo
          videoSrc="/webgl/stars.mp4"
          flipX={true}
          flipY={true}
          className="w-full h-full"
        />
      </div>

      {/* Content layer */}
      <div className="relative z-10 w-full h-full flex items-center justify-center">
        <AnimatePresence mode="wait">
          {currentStep === "cli-setup" ? (
            <TerminalAnimationWithLogs key="terminal" />
          ) : currentStep === "toolset" ? (
            <ToolsetAnimation key="toolset" toolsetName={toolsetName} />
          ) : currentStep === "mcp" ? (
            <McpAnimation key="mcp" mcpSlug={mcpSlug} />
          ) : (
            <DefaultLogo key="default" />
          )}
        </AnimatePresence>
      </div>
    </div>
  );
};

const DefaultLogo = () => (
  <motion.div
    className="w-32 h-32 bg-card rounded-lg border flex items-center justify-center"
    exit={{
      y: [0, -20, -8],
      transition: { duration: 0.5, times: [0, 0.4, 1], ease: "easeInOut" },
    }}
  >
    <motion.span className="font-thin text-foreground text-6xl select-none">
      <GramLogo className="w-18" variant="icon" />
    </motion.span>
  </motion.div>
);

const TerminalSpinner = () => {
  const spinnerFrames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
  const [frame, setFrame] = useState(0);

  useEffect(() => {
    const interval = setInterval(() => {
      setFrame((prev) => (prev + 1) % spinnerFrames.length);
    }, 80);
    return () => clearInterval(interval);
  }, []);

  return <span className="text-primary">{spinnerFrames[frame]}</span>;
};

const TerminalAnimationWithLogs = () => {
  const [hasMoved, setHasMoved] = useState(false);
  const terminalX = useMotionValue(-75);
  const terminalY = useMotionValue(75);
  const editorX = useMotionValue(75);
  const editorY = useMotionValue(-75);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [deploymentStatus, setDeploymentStatus] = useState<
    "none" | "processing" | "complete"
  >("none");
  const [focusedWindow, setFocusedWindow] = useState<"terminal" | "editor">(
    "terminal",
  );
  const [hasChangedFocus, setHasChangedFocus] = useState(false);
  const [animationComplete, setAnimationComplete] = useState(false);
  const terminalContentRef = useRef<HTMLDivElement>(null);
  const [animationKey, setAnimationKey] = useState(0);
  const [editorFilename, setEditorFilename] = useState("src/mcp.ts");
  const [editorCode, setEditorCode] = useState<string | null>(null);

  const { data: tools } = useListTools(undefined, undefined, {
    refetchInterval: deploymentStatus !== "complete" ? 2000 : false,
  });

  // Easter egg: MCP TypeScript SDK code sample
  const mcpCode = `import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

export const server = new McpServer({
  name: "demo-server",
  version: "1.0.0",
});

server.registerTool(
  "add",
  {
    title: "Addition Tool",
    description: "Add two numbers",
    inputSchema: { a: z.number(), b: z.number() },
  },
  async ({ a, b }) => {
    const output = { result: a + b };
    return {
      content: [{ type: "text", text: JSON.stringify(output) }],
    };
  },
);`;

  const greetCode = `import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const gram = new Gram().tool({
  name: "greet",
  description: "Greet someone special",
  inputSchema: { name: z.string() },
  async execute(ctx, input) {
    return ctx.json({
      message: \`Hello, \${input.name}!\`
    });
  },
});

export default gram;`;

  // Initialize editor with MCP code
  useEffect(() => {
    setEditorCode(mcpCode);
  }, []);

  // Animate logs appearing one by one
  useEffect(() => {
    // Pause animation if user has dragged windows
    if (hasMoved) {
      return;
    }

    const timers: NodeJS.Timeout[] = [];
    let cumulativeDelay = 0;

    DEMO_LOGS.forEach((log, index) => {
      const timer = setTimeout(() => {
        setLogs((prev) => [...prev, log]);

        // At "open ." command (index 7, id "8"), switch to functions.ts and type code
        if (index === 7) {
          const editorTimeout = setTimeout(() => {
            setFocusedWindow("editor");
            // Clear editor and change filename
            setEditorCode(greetCode);
            setEditorFilename("src/functions.ts");

            // Type out the greet function code
            let charIndex = 0;
            const typeInterval = setInterval(() => {
              if (charIndex < greetCode.length) {
                setEditorCode(greetCode.slice(0, charIndex + 1));
                charIndex++;
              } else {
                clearInterval(typeInterval);
                // After typing completes, refocus terminal
                const refocusTimeout = setTimeout(() => {
                  setFocusedWindow("terminal");
                }, 800);
                timers.push(refocusTimeout);
              }
            }, 20);
            timers.push(typeInterval as unknown as NodeJS.Timeout);
          }, 300);
          timers.push(editorTimeout);
        }

        // Mark animation complete when last log appears
        if (index === DEMO_LOGS.length - 1) {
          const completeTimeout = setTimeout(() => {
            setAnimationComplete(true);
          }, 1000);
          timers.push(completeTimeout);
        }
      }, cumulativeDelay);
      timers.push(timer);

      // Calculate next delay
      if (index === 7) {
        // After "open ." wait for editor focus + typing + pause before continuing terminal
        cumulativeDelay += 800 + 300 + greetCode.length * 20 + 800;
      } else {
        cumulativeDelay += 800;
      }
    });

    return () => timers.forEach(clearTimeout);
  }, [animationKey, hasMoved]);

  // Check for actual tools deployment
  useEffect(() => {
    const hasTools = tools?.tools && tools.tools.length > 0;

    if (hasTools && deploymentStatus === "none") {
      setDeploymentStatus("processing");

      setTimeout(() => {
        setDeploymentStatus("complete");
        setLogs((prev) =>
          prev.map((log) =>
            log.id === "14"
              ? {
                  id: log.id,
                  message: "✓ Push successful",
                  type: "success" as const,
                }
              : log,
          ),
        );
      }, 500);
    }
  }, [tools, deploymentStatus]);

  // Auto-scroll terminal to bottom when logs change
  useEffect(() => {
    if (terminalContentRef.current) {
      terminalContentRef.current.scrollTop =
        terminalContentRef.current.scrollHeight;
    }
  }, [logs]);

  useEffect(() => {
    const unsubscribeTerminalX = terminalX.on("change", (latest) => {
      if (!hasMoved && Math.abs(latest - -75) > 5) {
        setHasMoved(true);
      }
    });
    const unsubscribeTerminalY = terminalY.on("change", (latest) => {
      if (!hasMoved && Math.abs(latest - 75) > 5) {
        setHasMoved(true);
      }
    });
    const unsubscribeEditorX = editorX.on("change", (latest) => {
      if (!hasMoved && Math.abs(latest - 75) > 5) {
        setHasMoved(true);
      }
    });
    const unsubscribeEditorY = editorY.on("change", (latest) => {
      if (!hasMoved && Math.abs(latest - -75) > 5) {
        setHasMoved(true);
      }
    });

    return () => {
      unsubscribeTerminalX();
      unsubscribeTerminalY();
      unsubscribeEditorX();
      unsubscribeEditorY();
    };
  }, [hasMoved, terminalX, terminalY, editorX, editorY]);

  // Sync window positions to WebGL store for shader effects
  const setDraggableWindowPosition = useWebGLStore(
    (state) => state.setDraggableWindowPosition,
  );
  const setIsDraggingWindow = useWebGLStore(
    (state) => state.setIsDraggingWindow,
  );

  useEffect(() => {
    const updateTerminalPosition = () => {
      setDraggableWindowPosition("terminal", {
        x: terminalX.get(),
        y: terminalY.get(),
        width: 550,
        height: 350,
      });
    };

    const updateEditorPosition = () => {
      setDraggableWindowPosition("editor", {
        x: editorX.get(),
        y: editorY.get(),
        width: 550,
        height: 350,
      });
    };

    const unsubscribeTerminalX = terminalX.on("change", updateTerminalPosition);
    const unsubscribeTerminalY = terminalY.on("change", updateTerminalPosition);
    const unsubscribeEditorX = editorX.on("change", updateEditorPosition);
    const unsubscribeEditorY = editorY.on("change", updateEditorPosition);

    // Initialize positions
    updateTerminalPosition();
    updateEditorPosition();

    return () => {
      unsubscribeTerminalX();
      unsubscribeTerminalY();
      unsubscribeEditorX();
      unsubscribeEditorY();
    };
  }, [terminalX, terminalY, editorX, editorY, setDraggableWindowPosition]);

  const handleReset = () => {
    terminalX.set(-75);
    terminalY.set(75);
    editorX.set(75);
    editorY.set(-75);
    setHasMoved(false);
    setHasChangedFocus(false);
    setAnimationComplete(false);
    setFocusedWindow("terminal");
    // Restart animation
    setLogs([]);
    setEditorCode(mcpCode);
    setEditorFilename("src/mcp.ts");
    setAnimationKey((prev) => prev + 1);
  };

  const handleWindowClick = (window: "terminal" | "editor") => {
    setFocusedWindow(window);
    setHasChangedFocus(true);
  };

  if (!editorCode) {
    return null;
  }

  return (
    <div className="relative w-full h-full flex items-center justify-center scale-[0.65] lg:scale-75 xl:scale-90">
      {/* Terminal Window - positioned bottom-left */}
      <motion.div
        drag
        dragMomentum={false}
        dragElastic={0}
        dragConstraints={{
          left: -400,
          right: 1000,
          top: -1000,
          bottom: 1000,
        }}
        onDragStart={() => {
          handleWindowClick("terminal");
          setIsDraggingWindow(true);
        }}
        onDragEnd={() => setIsDraggingWindow(false)}
        className={cn(
          "absolute w-[550px] bg-card border rounded-lg overflow-hidden cursor-pointer",
          focusedWindow === "terminal" ? "z-20 shadow-xl" : "z-10 shadow-sm",
        )}
        style={{ x: terminalX, y: terminalY }}
        onClick={() => handleWindowClick("terminal")}
      >
        {/* Terminal header */}
        <div className="bg-muted border-b px-4 py-2 flex items-center justify-between cursor-grab active:cursor-grabbing">
          <div className="flex gap-1.5">
            <div
              className={cn(
                "w-3 h-3 rounded-full",
                focusedWindow === "terminal"
                  ? "bg-red-500/80"
                  : "bg-muted-foreground/30",
              )}
            />
            <div
              className={cn(
                "w-3 h-3 rounded-full",
                focusedWindow === "terminal"
                  ? "bg-yellow-500/80"
                  : "bg-muted-foreground/30",
              )}
            />
            <div
              className={cn(
                "w-3 h-3 rounded-full",
                focusedWindow === "terminal"
                  ? "bg-green-500/80"
                  : "bg-muted-foreground/30",
              )}
            />
          </div>
          <Type
            small
            className="text-muted-foreground absolute left-1/2 -translate-x-1/2"
          >
            gram-cli {deploymentStatus !== "none" ? "• connected" : ""}
          </Type>
          <div className="w-[42px]" />
        </div>

        {/* Terminal content */}
        <div
          ref={terminalContentRef}
          className="p-4 font-mono text-sm space-y-1 h-[350px] overflow-y-auto"
        >
          {logs.map((log) => {
            const shouldShowLoading = log.loading;

            return (
              <div
                key={log.id}
                className={cn(
                  log.type === "success" && "text-success-foreground",
                  log.type === "info" && "text-foreground",
                )}
              >
                {shouldShowLoading ? (
                  <>
                    <TerminalSpinner /> {log.message}
                  </>
                ) : log.loading ? null : (
                  log.message
                )}
              </div>
            );
          })}
        </div>
      </motion.div>

      {/* Editor Window - positioned top-right, always visible with MCP SDK code */}
      <motion.div
        drag
        dragMomentum={false}
        dragElastic={0}
        dragConstraints={{
          left: -400,
          right: 1000,
          top: -1000,
          bottom: 1000,
        }}
        onDragStart={() => {
          handleWindowClick("editor");
          setIsDraggingWindow(true);
        }}
        onDragEnd={() => setIsDraggingWindow(false)}
        style={{ x: editorX, y: editorY }}
        className={cn(
          "absolute w-[550px] bg-card border rounded-lg overflow-hidden cursor-pointer",
          focusedWindow === "editor" ? "z-30 shadow-xl" : "z-10 shadow-sm",
        )}
        onClick={() => handleWindowClick("editor")}
      >
        {/* Editor header */}
        <div className="bg-muted border-b px-4 py-2 flex items-center justify-between cursor-grab active:cursor-grabbing">
          <div className="flex gap-1.5">
            <div
              className={cn(
                "w-3 h-3 rounded-full",
                focusedWindow === "editor"
                  ? "bg-red-500/80"
                  : "bg-muted-foreground/30",
              )}
            />
            <div
              className={cn(
                "w-3 h-3 rounded-full",
                focusedWindow === "editor"
                  ? "bg-yellow-500/80"
                  : "bg-muted-foreground/30",
              )}
            />
            <div
              className={cn(
                "w-3 h-3 rounded-full",
                focusedWindow === "editor"
                  ? "bg-green-500/80"
                  : "bg-muted-foreground/30",
              )}
            />
          </div>
          <Type
            small
            className="text-muted-foreground absolute left-1/2 -translate-x-1/2"
          >
            {editorFilename}
          </Type>
          <div className="w-[42px]" />
        </div>

        {/* Editor content - starts with MCP SDK easter egg, switches to greet function */}
        <CodeBlock
          language="typescript"
          copyable={false}
          className="!p-0 !rounded-none !border-none"
          preClassName="!bg-transparent h-[350px] overflow-y-auto p-4 whitespace-pre-wrap"
        >
          {editorCode}
        </CodeBlock>
      </motion.div>

      {/* Reset button - show when animation complete or position/focus changed */}
      <AnimatePresence>
        {(animationComplete || hasMoved || hasChangedFocus) && (
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 10 }}
            transition={{ duration: 0.2 }}
            className="absolute bottom-8 right-8 z-50"
          >
            <Button
              variant="secondary"
              size="sm"
              onClick={handleReset}
              className="gap-2"
            >
              <RefreshCcw className="w-4 h-4" />
              Reset
            </Button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};

const ToolsetAnimation = ({
  toolsetName,
}: {
  toolsetName: string | undefined;
}) => {
  return (
    <div className="flex flex-col items-start gap-3">
      {/* Toolset name that appears after the main animation */}
      <motion.div
        initial={{ opacity: 0, y: -10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{
          delay: 0.5,
          type: "spring",
          duration: 0.4,
          bounce: 0.1,
        }}
        className="w-70 pl-1"
      >
        <h3 className="text-lg font-medium text-foreground mb-1">
          {toolsetName || "my-toolset"}
        </h3>
      </motion.div>

      {/* Main logo that morphs from the default logo */}
      <motion.div
        layoutId="main-container"
        className="w-70 h-12 bg-card rounded-lg border  flex items-center px-4"
        transition={{ type: "spring", duration: 0.6, bounce: 0.1 }}
      >
        <motion.div
          layoutId="main-icon"
          className="w-6 h-6 bg-background rounded flex items-center justify-center flex-shrink-0"
          transition={{ type: "spring", duration: 0.6, bounce: 0.1 }}
        >
          <motion.div
            initial={{ opacity: 0, scale: 0.5 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{
              delay: 0.3,
              type: "spring",
              duration: 0.4,
              bounce: 0.3,
            }}
          >
            <Wrench className="w-3 h-3 text-muted-foreground" />
          </motion.div>
        </motion.div>
        <motion.div
          initial={{ opacity: 0, x: -10 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{
            delay: 0.2,
            type: "spring",
            duration: 0.5,
            bounce: 0.2,
          }}
          className="ml-3 flex-1"
        >
          <div className="h-3 bg-muted rounded w-full" />
        </motion.div>
      </motion.div>

      {/* Additional tool items that stagger in */}
      <AnimatePresence>
        <motion.div
          initial={{ opacity: 0, y: 20, scale: 0.8 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          transition={{
            delay: 0.4,
            type: "spring",
            duration: 0.5,
            bounce: 0.2,
          }}
          className="w-70 h-12 bg-card rounded-lg border flex items-center px-4"
        >
          <div className="w-6 h-6 bg-background rounded flex items-center justify-center">
            <Wrench className="w-3 h-3 text-muted-foreground" />
          </div>
          <div className="ml-3 h-3 bg-muted rounded w-full" />
        </motion.div>

        <motion.div
          initial={{ opacity: 0, y: 20, scale: 0.8 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          transition={{
            delay: 0.6,
            type: "spring",
            duration: 0.5,
            bounce: 0.2,
          }}
          className="w-70 h-12 bg-card rounded-lg border  flex items-center px-4"
        >
          <div className="w-6 h-6 bg-background rounded flex items-center justify-center">
            <Wrench className="w-3 h-3 text-muted-foreground" />
          </div>
          <div className="ml-3 h-3 bg-muted rounded w-full" />
        </motion.div>
      </AnimatePresence>
    </div>
  );
};

const McpAnimation = ({ mcpSlug }: { mcpSlug: string | undefined }) => {
  const slug = mcpSlug
    ? `https://app.getgram.ai/mcp/${mcpSlug}`
    : `https://app.getgram.ai/mcp/my-toolset`;

  return (
    <div className="flex flex-col items-center gap-4">
      {/* Server rack units */}
      <div className="flex flex-col items-center gap-2">
        {/* First tool transforms into server rack unit */}
        <motion.div
          layoutId="main-container"
          className="w-48 h-10 bg-card rounded-lg border  flex items-center px-4"
          transition={{ type: "spring", duration: 0.6, bounce: 0.1 }}
        >
          <motion.div
            layoutId="main-icon"
            className="flex items-center justify-center flex-shrink-0"
            transition={{ type: "spring", duration: 0.6, bounce: 0.1 }}
          >
            <motion.div
              initial={{ opacity: 0, scale: 0.5 }}
              animate={{ opacity: 1, scale: 1 }}
              transition={{
                delay: 0.3,
                type: "spring",
                duration: 0.4,
                bounce: 0.3,
              }}
              className="w-3 h-3 bg-muted-foreground rounded-full"
            />
          </motion.div>
        </motion.div>

        {/* Second server rack unit */}
        <motion.div
          initial={{ opacity: 0, y: 20, scale: 0.8 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          transition={{
            delay: 0.4,
            type: "spring",
            duration: 0.5,
            bounce: 0.2,
          }}
          className="w-48 h-10 bg-card rounded-lg border  flex items-center px-4"
        >
          <div className="w-3 h-3 bg-muted-foreground rounded-full" />
        </motion.div>
      </div>

      {/* Slug label below the server rack */}
      <motion.div
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{
          delay: 0.6,
          type: "spring",
          duration: 0.4,
          bounce: 0.1,
        }}
        className="text-center"
      >
        <div className="bg-background border rounded-md px-3 py-2">
          <p className="text-sm font-mono text-muted-foreground">{slug}</p>
        </div>
      </motion.div>
    </div>
  );
};

const TwitterIcon = ({ className }: { className?: string }) => {
  return (
    <svg
      role="img"
      viewBox="0 0 24 24"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      <title>X</title>
      <path d="M18.901 1.153h3.68l-8.04 9.19L24 22.846h-7.406l-5.8-7.584-6.638 7.584H.474l8.6-9.83L0 1.154h7.594l5.243 6.932ZM17.61 20.644h2.039L6.486 3.24H4.298Z" />
    </svg>
  );
};
