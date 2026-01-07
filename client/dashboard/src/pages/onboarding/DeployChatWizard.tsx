import { CodeBlock } from "@/components/code";
import { GramLogo } from "@/components/gram-logo";
import { InputField } from "@/components/moon/input-field";
import { ProjectSelector } from "@/components/project-menu";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { Button, Stack } from "@speakeasy-api/moonshine";
import {
  Check,
  ChevronRight,
  Code,
  MessageSquare,
  Rocket,
  Settings,
} from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { cn } from "@/lib/utils";

type DeployChatStep = "setup" | "configure" | "deploy";

export function DeployChatWizard() {
  const [currentStep, setCurrentStep] = useState<DeployChatStep>("setup");
  const [projectName, setProjectName] = useState<string>("");

  return (
    <Stack direction={"horizontal"} className="h-[100vh] w-full">
      <div className="w-1/2 h-full border-r-1">
        <DeployChatLHS
          currentStep={currentStep}
          setCurrentStep={setCurrentStep}
          projectName={projectName}
          setProjectName={setProjectName}
        />
      </div>
      <div className="w-1/2 h-full bg-background overflow-hidden flex items-center justify-center">
        <DeployChatRHS currentStep={currentStep} projectName={projectName} />
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

const DeployChatLHS = ({
  currentStep,
  setCurrentStep,
  projectName,
  setProjectName,
}: {
  currentStep: DeployChatStep;
  setCurrentStep: (step: DeployChatStep) => void;
  projectName: string;
  setProjectName: (name: string) => void;
}) => {
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
          <Link className="hover:bg-accent p-2 rounded-md" to="/">
            <GramLogo className="w-25" />
          </Link>
          <a href="https://docs.getgram.ai/" target="_blank">
            <Type mono className="text-[15px] font-normal">
              VIEW DOCS
            </Type>
          </a>
        </Stack>
        <Stack direction={"horizontal"} gap={6} align={"center"}>
          <Step
            text="Setup Project"
            icon={<Code className="w-4 h-4" />}
            active={currentStep === "setup"}
            completed={currentStep === "configure" || currentStep === "deploy"}
          />
          <ChevronRight className="w-4 h-4 text-muted-foreground" />
          <Step
            text="Configure Chat"
            icon={<Settings className="w-4 h-4" />}
            active={currentStep === "configure"}
            completed={currentStep === "deploy"}
          />
          <ChevronRight className="w-4 h-4 text-muted-foreground" />
          <Step
            text="Deploy"
            icon={<Rocket className="w-4 h-4" />}
            active={currentStep === "deploy"}
          />
        </Stack>
      </Stack>

      {/* Content */}
      <div
        className="absolute inset-x-0 bottom-16 pointer-events-none"
        style={{ top: "160px" }}
      >
        <div className="h-full overflow-y-auto px-16 flex items-center justify-center">
          <Stack className="w-full max-w-3xl gap-8 pointer-events-auto z-10 my-auto">
            {currentStep === "setup" && (
              <SetupStep
                setCurrentStep={setCurrentStep}
                projectName={projectName}
                setProjectName={setProjectName}
              />
            )}
            {currentStep === "configure" && (
              <ConfigureStep setCurrentStep={setCurrentStep} />
            )}
            {currentStep === "deploy" && <DeployStep />}
          </Stack>
        </div>
      </div>

      {/* Footer */}
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

const SetupStep = ({
  setCurrentStep,
  projectName,
  setProjectName,
}: {
  setCurrentStep: (step: DeployChatStep) => void;
  projectName: string;
  setProjectName: (name: string) => void;
}) => {
  const [installMethod, setInstallMethod] = useState<"npm" | "pnpm">("npm");

  const commands = [
    {
      label: "Create a new Gram Chat project",
      command: `${installMethod} create @gram-ai/chat@latest`,
      showToggle: true,
    },
    {
      label: "Install dependencies",
      command: `cd my-chat-app && ${installMethod} install`,
    },
    {
      label: "Start the development server",
      command: `${installMethod === "npm" ? "npm run" : "pnpm"} dev`,
    },
  ];

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Deploy Data-Integrated Chat</span>
        <span className="text-body-sm">
          Create an embeddable chat experience powered by your data
        </span>
      </Stack>

      <InputField
        label="Project Name"
        placeholder="my-chat-app"
        value={projectName}
        onChange={(e) => setProjectName(e.target.value)}
        hint="Give your chat project a name"
      />

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
            <CodeBlock language="bash">{item.command}</CodeBlock>
          </Stack>
        ))}
      </Stack>

      <Button
        variant="brand"
        className="w-full"
        onClick={() => setCurrentStep("configure")}
        disabled={!projectName}
      >
        Continue
      </Button>
    </>
  );
};

const ConfigureStep = ({
  setCurrentStep,
}: {
  setCurrentStep: (step: DeployChatStep) => void;
}) => {
  const configCode = `// gram.config.ts
import { defineConfig } from "@gram-ai/chat";

export default defineConfig({
  // Connect to your MCP server
  mcpServer: "https://mcp.getgram.ai/your-org/your-server",

  // Customize the chat experience
  theme: {
    primaryColor: "#6366f1",
    borderRadius: "lg",
  },

  // Configure available tools
  tools: {
    // Tools are automatically loaded from your MCP server
    enabled: true,
  },

  // Optional: Add authentication
  auth: {
    required: false,
  },
});`;

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Configure Your Chat</span>
        <span className="text-body-sm">
          Customize the chat experience and connect to your MCP server
        </span>
      </Stack>

      <Stack gap={2}>
        <Type small className="font-medium">
          Edit your configuration file:
        </Type>
        <CodeBlock language="typescript">{configCode}</CodeBlock>
      </Stack>

      <Stack gap={2}>
        <Type small className="font-medium">
          Key configuration options:
        </Type>
        <ul className="list-disc list-inside text-body-sm text-muted-foreground space-y-1">
          <li>
            <strong>mcpServer</strong> - Your Gram MCP server URL
          </li>
          <li>
            <strong>theme</strong> - Customize colors, fonts, and styling
          </li>
          <li>
            <strong>tools</strong> - Configure which tools are available
          </li>
          <li>
            <strong>auth</strong> - Add authentication requirements
          </li>
        </ul>
      </Stack>

      <Button
        variant="brand"
        className="w-full"
        onClick={() => setCurrentStep("deploy")}
      >
        Continue
      </Button>
    </>
  );
};

const DeployStep = () => {
  const routes = useRoutes();

  const deployCommands = [
    {
      label: "Build for production",
      command: "npm run build",
    },
    {
      label: "Deploy to Vercel (or your preferred platform)",
      command: "vercel deploy",
    },
  ];

  const embedCode = `<!-- Add to your website -->
<script src="https://chat.getgram.ai/embed.js"></script>
<gram-chat
  server="your-server-slug"
  theme="light"
/>`;

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">Deploy Your Chat</span>
        <span className="text-body-sm">
          Build and deploy your chat experience
        </span>
      </Stack>

      <Stack gap={4}>
        {deployCommands.map((item, index) => (
          <Stack key={index} gap={2}>
            <Type small className="font-medium">
              {index + 1}. {item.label}
            </Type>
            <CodeBlock language="bash">{item.command}</CodeBlock>
          </Stack>
        ))}
      </Stack>

      <Stack gap={2}>
        <Type small className="font-medium">
          Or embed directly in your website:
        </Type>
        <CodeBlock language="html">{embedCode}</CodeBlock>
      </Stack>

      <Button variant="brand" className="w-full" onClick={() => routes.home.goTo()}>
        Go to Dashboard
      </Button>
    </>
  );
};

const DeployChatRHS = ({
  currentStep,
  projectName,
}: {
  currentStep: DeployChatStep;
  projectName: string;
}) => {
  return (
    <div className="flex flex-col items-center gap-4">
      {/* Chat preview mockup */}
      <div className="w-80 bg-card border rounded-lg shadow-lg overflow-hidden">
        {/* Chat header */}
        <div className="bg-muted border-b px-4 py-3 flex items-center gap-2">
          <MessageSquare className="w-5 h-5 text-primary" />
          <span className="font-medium text-sm">
            {projectName || "My Chat App"}
          </span>
        </div>

        {/* Chat messages */}
        <div className="p-4 space-y-3 h-64">
          <div className="flex justify-end">
            <div className="bg-primary text-primary-foreground rounded-lg px-3 py-2 max-w-[80%] text-sm">
              What can you help me with?
            </div>
          </div>
          <div className="flex justify-start">
            <div className="bg-muted rounded-lg px-3 py-2 max-w-[80%] text-sm">
              I can help you with anything related to your data! I have access
              to your tools and can perform actions on your behalf.
            </div>
          </div>
          {currentStep !== "setup" && (
            <div className="flex justify-end">
              <div className="bg-primary text-primary-foreground rounded-lg px-3 py-2 max-w-[80%] text-sm">
                Show me recent activity
              </div>
            </div>
          )}
          {currentStep === "deploy" && (
            <div className="flex justify-start">
              <div className="bg-muted rounded-lg px-3 py-2 max-w-[80%] text-sm">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <div className="w-2 h-2 bg-primary rounded-full animate-pulse" />
                  Fetching data...
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Chat input */}
        <div className="border-t p-3">
          <div className="bg-muted rounded-lg px-3 py-2 text-sm text-muted-foreground">
            Type a message...
          </div>
        </div>
      </div>

      {/* Step indicator */}
      <Type small className="text-muted-foreground">
        {currentStep === "setup" && "Preview of your chat widget"}
        {currentStep === "configure" && "Customizing your chat experience"}
        {currentStep === "deploy" && "Ready to deploy!"}
      </Type>
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
