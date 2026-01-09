import { CodeBlock } from "@/components/code";
import { GramLogo } from "@/components/gram-logo";
import { ProjectSelector } from "@/components/project-menu";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { Loader2, MessageSquare } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";

export function DeployChatWizard() {
  return (
    <Stack direction={"horizontal"} className="h-[100vh] w-full">
      <div className="w-1/2 h-full border-r-1">
        <DeployChatLHS />
      </div>
      <div className="w-1/2 h-full bg-background overflow-hidden flex items-center justify-center">
        <DeployChatRHS />
      </div>
    </Stack>
  );
}

const DeployChatLHS = () => {
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
      </Stack>

      {/* Content */}
      <div
        className="absolute inset-x-0 bottom-16 pointer-events-none"
        style={{ top: "160px" }}
      >
        <div className="h-full overflow-y-auto px-16 flex items-center justify-center">
          <Stack className="w-full max-w-3xl gap-8 pointer-events-auto z-10 my-auto">
            <SetupStep />
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

const SetupStep = () => {
  const routes = useRoutes();
  const { projectSlug } = useSlugs();
  const [apiKey, setApiKey] = useState<string | null>(null);

  const createKeyMutation = useCreateAPIKeyMutation({
    onSuccess: (data) => {
      if (data.key) {
        setApiKey(data.key);
      }
    },
  });

  const handleCreateApiKey = () => {
    createKeyMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createKeyForm: {
          // Add this random suffix or else a second key creation will cause a conflict and fail
          name: `Elements Chat - ${Math.random().toString(36).substring(2, 7).toUpperCase()}`,
          scopes: ["chat"],
        },
      },
    });
  };

  const installCommand = `pnpm add @gram-ai/elements`;

  const appCode = `import { GramElementsProvider, Chat, type ElementsConfig } from '@gram-ai/elements'
import '@gram-ai/elements/elements.css'

const config: ElementsConfig = {
  projectSlug: '${projectSlug}',
  api: {
    // TODO: Replace with Chat Sessions (see Gram docs) before shipping to production
    UNSAFE_apiKey: '${apiKey ?? "YOUR_API_KEY"}',
  }
}

export const App = () => {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  )
}`;

  return (
    <>
      <Stack gap={1}>
        <span className="text-heading-md">
          Deploy Chat Connected To Your Data
        </span>
        <span className="text-body-sm">
          Create an embeddable chat experience powered by your data
        </span>
      </Stack>

      <Stack gap={4}>
        <Stack gap={2}>
          <Type small className="font-medium">
            1. Create an API key if you don't have one yet
          </Type>
          {apiKey ? (
            <CodeBlock language="bash">{apiKey}</CodeBlock>
          ) : (
            <Button
              variant="secondary"
              onClick={handleCreateApiKey}
              disabled={createKeyMutation.isPending}
            >
              {createKeyMutation.isPending && (
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              )}
              Create API key
            </Button>
          )}
        </Stack>

        <Stack gap={2}>
          <Type small className="font-medium">
            2. Install the @gram-ai/elements package
          </Type>
          <CodeBlock language="bash">{installCommand}</CodeBlock>
        </Stack>

        <Stack gap={2}>
          <Type small className="font-medium">
            3. Add the provider and chat component to your app
          </Type>
          <CodeBlock language="tsx">{appCode}</CodeBlock>
        </Stack>
      </Stack>

      <routes.chatElements.Link>
        <Button
          variant="brand"
          className="w-full"
          onClick={() => {
            localStorage.setItem("elements-onboarding-completed", "true");
          }}
        >
          Continue
        </Button>
      </routes.chatElements.Link>
    </>
  );
};

const DeployChatRHS = () => {
  return (
    <div className="flex flex-col items-center gap-4">
      {/* Chat preview mockup */}
      <div className="w-80 bg-card border rounded-lg shadow-lg overflow-hidden">
        {/* Chat header */}
        <div className="bg-muted border-b px-4 py-3 flex items-center gap-2">
          <MessageSquare className="w-5 h-5 text-primary" />
          <span className="font-medium text-sm">My Chat App</span>
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
        Preview of your chat widget
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
