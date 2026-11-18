import { useInsightsState } from "@/components/insights-context";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useProject } from "@/contexts/Auth";
import { useAssistantsAvailable } from "@/hooks/useAssistantsAvailability";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { useEffect } from "react";
import { useNavigate } from "react-router";

export interface Investigation {
  /** What the person asks, as it appears in the chat. */
  question: string;
  /** The facts on screen, sent as Additional context. */
  context: string;
}

/** Sends a question with page context to the Project Assistant, like Explore with AI. */
function useInvestigate(): (investigation: Investigation) => void {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { available, isExpanded, setIsExpanded, setOverride, sendPrompt } =
    useInsightsState();

  // The context belongs to this question; once the dock closes it goes.
  useEffect(() => {
    if (!isExpanded) setOverride(null);
  }, [isExpanded, setOverride]);

  return ({ question, context }) => {
    setOverride({ contextInfo: context });
    if (!available) {
      sendPrompt(question);
      void navigate(routes.chat.conversation.href("new"));
      return;
    }
    setIsExpanded(true);
    sendPrompt(question);
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
        investigate(investigation);
        onLaunched?.();
      }}
    >
      <Button.LeftIcon>
        <Icon name="sparkles" className="size-4" />
      </Button.LeftIcon>
      <Button.Text>Ask AI to investigate</Button.Text>
    </Button>
  );
}
