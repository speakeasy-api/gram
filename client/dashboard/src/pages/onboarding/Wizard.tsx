import { Expandable } from "@/components/expandable";
import { AnyField } from "@/components/moon/any-field";
import { InputField } from "@/components/moon/input-field";
import { ProjectSelector } from "@/components/project-menu";
import { ToolBadge } from "@/components/tool-badge";
import { ErrorAlert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@speakeasy-api/moonshine";
import { Input } from "@/components/ui/input";
import { SkeletonParagraph } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Type } from "@/components/ui/type";
import FileUpload from "@/components/upload";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useApiError } from "@/hooks/useApiError";
import { slugify } from "@/lib/constants";
import { useGroupedHttpTools } from "@/lib/toolNames";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import {
  invalidateAllLatestDeployment,
  invalidateAllListToolsets,
  invalidateAllToolset,
  useListTools,
} from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  ChevronRight,
  CircleCheckIcon,
  FileJson2,
  ServerCog,
  Upload,
  Wrench,
  X,
} from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useState } from "react";
import { useParams } from "react-router";
import { toast } from "sonner";
import { useMcpSlugValidation } from "../mcp/MCPDetails";
import { DeploymentLogs, useOnboardingSteps } from "./Onboarding";
import { GramLogo } from "@/components/gram-logo";

export function OnboardingWizard() {
  const { orgSlug } = useParams();

  const [currentStep, setCurrentStep] = useState<"upload" | "toolset" | "mcp">(
    "upload"
  );
  const [toolsetName, setToolsetName] = useState<string>();
  const [mcpSlug, setMcpSlug] = useState<string>();

  // Initialize mcpSlug when toolsetName changes
  useEffect(() => {
    if (toolsetName && !mcpSlug) {
      setMcpSlug(`${orgSlug}-${toolsetName}`);
    }
  }, [toolsetName, mcpSlug]);

  return (
    <Stack direction={"horizontal"} className="h-[100vh] w-full">
      <div className="w-1/2 h-full border-r-1 ">
        <LHS
          currentStep={currentStep}
          setCurrentStep={setCurrentStep}
          toolsetName={toolsetName}
          setToolsetName={setToolsetName}
          mcpSlug={mcpSlug}
          setMcpSlug={setMcpSlug}
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
          "rounded-full bg-muted h-8 w-8 flex items-center justify-center",
          active && "bg-success text-success-foreground"
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

const LHS = ({
  currentStep,
  setCurrentStep,
  toolsetName,
  setToolsetName,
  mcpSlug,
  setMcpSlug,
}: {
  currentStep: "upload" | "toolset" | "mcp";
  setCurrentStep: (step: "upload" | "toolset" | "mcp") => void;
  toolsetName: string | undefined;
  setToolsetName: (name: string) => void;
  mcpSlug: string | undefined;
  setMcpSlug: (slug: string) => void;
}) => {
  const [createdToolset, setCreatedToolset] = useState<Toolset>();
  const { organization } = useSession();

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
          <GramLogo className="w-25" />
          <a href="https://docs.getgram.ai/" target="_blank">
            <Type mono className="text-[15px] font-normal">
              VIEW DOCS
            </Type>
          </a>
        </Stack>
        <Stack direction={"horizontal"} gap={6} align={"center"}>
          <Step
            text="Upload OpenAPI"
            icon={<Upload className="w-4 h-4" />}
            active={currentStep === "upload"}
            completed={currentStep !== "upload"}
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
      </Stack>

      {/* Content - absolutely positioned within left container */}
      <div className="absolute inset-x-0 top-0 bottom-0 flex items-center justify-center px-16 pointer-events-none">
        <Stack className="w-full max-w-3xl gap-8 pointer-events-auto">
          {currentStep === "upload" && (
            <UploadStep
              setCurrentStep={setCurrentStep}
              setToolsetName={setToolsetName}
            />
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
    createDeployment,
    apiName,
    setApiName,
    apiNameError,
    undoSpecUpload,
  } = useOnboardingSteps(false);

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
          <CircleCheckIcon className="w-4 h-4 text-emerald-500 dark:text-success" />
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
    <FileUpload
      label={<span className="text-body-sm">Drop your OpenAPI spec here</span>}
      onUpload={handleSpecUpload}
      allowedExtensions={["yaml", "yml", "json"]}
      className="max-w-full"
    />
  );

  const onContinue = async () => {
    setToolsetName(slugify(apiName || "my-toolset"));
    const deployment = await createDeployment(undefined, true);

    if (deployment?.toolCount === 0 || deployment?.status === "failed") {
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
  const { handleApiError } = useApiError();
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
          httpToolNames: tools?.tools.map((tool) => tool.name) ?? [],
        },
      });

      setCreatedToolset(toolset);
      setCurrentStep("mcp");
    } catch (error) {
      const errorMessage =
        error instanceof Error ? error.message : "Failed to create toolset";
      setCreateError(errorMessage);
      handleApiError(error, "Failed to create toolset");
    }
  };

  const groupedTools = useGroupedHttpTools(tools?.tools ?? []);
  const flattened = groupedTools.flatMap((group) => group.tools);
  const toolsToShow = flattened.slice(0, 25);
  const additionalTools = flattened.slice(25);

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Create Toolset</span>
        <span className="text-body-sm">Give this toolset a name</span>
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
  const { handleApiError } = useApiError();
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
      handleApiError(error, "Failed to setup MCP server");
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
        } catch (error) {
          // Error is already handled by the individual step components
          console.error("Button click error:", error);
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
  currentStep: "upload" | "toolset" | "mcp";
  toolsetName: string | undefined;
  mcpSlug: string | undefined;
}) => {
  return (
    <div className="w-full h-full bg-background flex items-center justify-center relative overflow-hidden">
      <AnimatePresence mode="wait">
        {currentStep === "toolset" ? (
          <ToolsetAnimation key="toolset" toolsetName={toolsetName} />
        ) : currentStep === "mcp" ? (
          <McpAnimation key="mcp" mcpSlug={mcpSlug} />
        ) : (
          <DefaultLogo key="default" />
        )}
      </AnimatePresence>
    </div>
  );
};

const DefaultLogo = () => (
  <motion.div
    layoutId="main-container"
    className="w-32 h-32 bg-card rounded-lg border flex items-center justify-center"
    transition={{ type: "spring", duration: 0.6, bounce: 0.1 }}
  >
    <motion.span
      layoutId="main-icon"
      className="font-thin text-foreground text-6xl select-none"
      transition={{ type: "spring", duration: 0.6, bounce: 0.1 }}
    >
      <GramLogo className="w-18" variant="icon" />
    </motion.span>
  </motion.div>
);

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
