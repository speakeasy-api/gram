import { useInsightsState } from "@/components/insights-context";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useProject } from "@/contexts/Auth";
import { useAssistantsAvailable } from "@/hooks/useAssistantsAvailability";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { useNavigate } from "react-router";

export interface Investigation {
  /** What the person asks, as it appears in the chat. */
  question: string;
  /** The facts on screen, sent as Additional context. */
  context: string;
}

/** Context rides in the message's tagged block, which the chat shows as Additional context. */
function investigationPrompt({ question, context }: Investigation): string {
  const safe = context.replaceAll(
    "</dashboard_context>",
    "&lt;/dashboard_context&gt;",
  );
  return `<dashboard_context>\n${safe}\n</dashboard_context>\n\n${question}`;
}

function useInvestigate(): (investigation: Investigation) => void {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { available, setIsExpanded, sendPrompt } = useInsightsState();
  return (investigation) => {
    const prompt = investigationPrompt(investigation);
    if (!available) {
      sendPrompt(prompt);
      void navigate(routes.chat.conversation.href("new"));
      return;
    }
    setIsExpanded(true);
    sendPrompt(prompt);
  };
}

/** "Ask AI to investigate", shown only to people who can use the assistant. */
export function AskAiButton({
  investigation,
  onLaunched,
}: {
  investigation: Investigation;
  /** Called once the question is sent, e.g. to close the sheet over the dock. */
  onLaunched?: () => void;
}): JSX.Element | null {
  const project = useProject();
  const { hasScope } = useRBAC();
  const assistantsAvailable = useAssistantsAvailable();
  const investigate = useInvestigate();
  if (
    !assistantsAvailable ||
    !hasScope("assistant:read", undefined, project.id)
  )
    return null;
  return (
    <Button
      variant="secondary"
      onClick={() => {
        // Close first: a later close would navigate back from the chat.
        onLaunched?.();
        investigate(investigation);
      }}
    >
      <Button.LeftIcon>
        <Icon name="sparkles" className="size-4" />
      </Button.LeftIcon>
      <Button.Text>Ask AI to investigate</Button.Text>
    </Button>
  );
}
