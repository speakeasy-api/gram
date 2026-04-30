import { Type } from "@/components/ui/type";
import { ResizablePanel } from "@speakeasy-api/moonshine";
import { Bot } from "lucide-react";

const SUGGESTIONS = [
  {
    primary: "Slack morning summary",
    secondary: "Summarize Slack DMs each morning",
  },
  {
    primary: "Slack on-mention bot",
    secondary: "Reply when @-mentioned in Slack",
  },
  { primary: "Periodic data sync", secondary: "Hit an API on a cron" },
];

export function AssistantsOnboardingPreview({
  onLoginPrompt,
}: {
  onLoginPrompt: () => void;
}) {
  return (
    <ResizablePanel
      direction="horizontal"
      className="[&>[role='separator']]:bg-neutral-softest [&>[role='separator']]:hover:bg-primary h-full [&>[role='separator']]:relative [&>[role='separator']]:w-px [&>[role='separator']]:border-0 [&>[role='separator']]:before:absolute [&>[role='separator']]:before:inset-y-0 [&>[role='separator']]:before:-right-1 [&>[role='separator']]:before:-left-1 [&>[role='separator']]:before:cursor-col-resize"
    >
      <ResizablePanel.Pane minSize={35} order={0}>
        <PreviewChatPane onLoginPrompt={onLoginPrompt} />
      </ResizablePanel.Pane>
      <ResizablePanel.Pane minSize={20} defaultSize={28}>
        <PreviewDraftPanel />
      </ResizablePanel.Pane>
    </ResizablePanel>
  );
}

function PreviewChatPane({ onLoginPrompt }: { onLoginPrompt: () => void }) {
  return (
    <div className="flex h-full flex-col items-center justify-between p-8">
      <div className="mt-12 max-w-2xl text-center">
        <p className="mb-2 text-2xl font-semibold text-stone-800 dark:text-stone-200">
          Build your assistant
        </p>
        <Type small muted>
          Tell me what you want this assistant to do — I&apos;ll create it, wire
          up the right tools, environments, and triggers, and walk you through
          any setup that needs your input.
        </Type>
      </div>
      <div className="w-full max-w-2xl space-y-4">
        <div className="flex flex-wrap justify-center gap-2">
          {SUGGESTIONS.map((s) => (
            <button
              key={s.primary}
              onClick={onLoginPrompt}
              className="hover:bg-accent rounded-full border px-4 py-2 transition-colors"
              type="button"
            >
              <span className="text-sm font-medium text-stone-800 dark:text-stone-200">
                {s.primary}
              </span>
              <span className="text-muted-foreground ml-1 text-sm">
                — {s.secondary}
              </span>
            </button>
          ))}
        </div>
        <button
          onClick={onLoginPrompt}
          type="button"
          className="hover:border-primary w-full cursor-pointer rounded-lg border p-4 text-left transition-colors"
        >
          <Type small muted>
            Describe what you want this assistant to do…
          </Type>
        </button>
      </div>
    </div>
  );
}

function PreviewDraftPanel() {
  return (
    <div className="bg-card flex h-full flex-col">
      <div className="border-b px-4 py-3">
        <Type variant="subheading">Draft assistant</Type>
      </div>
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
        <Bot className="text-muted-foreground h-10 w-10 opacity-40" />
        <Type small muted className="max-w-sm">
          Once you describe your assistant in the chat, the live spec will
          appear here as it&apos;s built.
        </Type>
      </div>
    </div>
  );
}
