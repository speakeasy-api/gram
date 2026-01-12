import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
} from "@/components/ai-elements/prompt-input";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import {
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useAssistantApi,
  useAssistantState,
} from "@assistant-ui/react";
import { useGramElements } from "@gram-ai/elements";
import { type FC } from "react";
import { usePlaygroundAuthWarning } from "./PlaygroundElements";
import { useRoutes } from "@/routes";
import { AlertCircle } from "lucide-react";

/**
 * Custom ThreadWelcome component using Gram design system.
 * Displays centered empty state with title, subtitle, and optional suggestions.
 */
export const GramThreadWelcome: FC = () => {
  const { config } = useGramElements();
  const { title, subtitle, suggestions } = config.welcome ?? {};

  return (
    <div className="flex size-full flex-col items-center justify-center gap-3 p-8 text-center">
      <div className="space-y-1">
        <Type variant="subheading" className="font-medium">
          {title}
        </Type>
        <Type variant="small" muted>
          {subtitle}
        </Type>
      </div>
      {suggestions && suggestions.length > 0 && (
        <div className="flex flex-wrap justify-center gap-2 mt-4">
          {suggestions.map((suggestion, index) => (
            <ThreadPrimitive.Suggestion
              key={index}
              prompt={suggestion.action}
              send
              asChild
            >
              <Button variant="outline" size="sm" className="rounded-full">
                {suggestion.title}
              </Button>
            </ThreadPrimitive.Suggestion>
          ))}
        </div>
      )}
    </div>
  );
};

/**
 * Custom UserMessage component using Gram design system.
 * Displays user messages as primary-colored bubbles aligned to the right.
 */
export const GramUserMessage: FC = () => (
  <MessagePrimitive.Root asChild>
    <div className="flex w-full justify-end py-4 px-2" data-role="user">
      <div className="flex flex-col gap-2 overflow-hidden rounded-lg max-w-[80%] bg-secondary px-4 py-3 text-foreground">
        <MessagePrimitive.Content />
      </div>
    </div>
  </MessagePrimitive.Root>
);

/**
 * Custom Composer component using Gram design system.
 * Uses InputGroup for consistent form styling with the dashboard.
 */
export const GramComposer: FC = () => {
  const { config } = useGramElements();
  const placeholder = config.composer?.placeholder ?? "Send a message...";

  return (
    <ComposerPrimitive.Root className="aui-composer-root relative flex w-full flex-col">
      <ComposerPrimitive.AttachmentDropzone className="aui-composer-attachment-dropzone flex w-full flex-col rounded-2xl border border-input bg-background px-1 pt-2 outline-none transition-shadow has-[textarea:focus-visible]:border-ring has-[textarea:focus-visible]:ring-2 has-[textarea:focus-visible]:ring-ring/20 data-[dragging=true]:border-ring data-[dragging=true]:border-dashed data-[dragging=true]:bg-accent/50">
        {/*<ComposerAttachments />*/}
        <ComposerPrimitive.Input
          placeholder={placeholder}
          className="aui-composer-input mb-1 max-h-32 min-h-14 w-full resize-none bg-transparent px-4 pt-2 pb-3 text-sm outline-none placeholder:text-muted-foreground focus-visible:ring-0"
          rows={1}
          autoFocus
          aria-label="Message input"
        />
      </ComposerPrimitive.AttachmentDropzone>
    </ComposerPrimitive.Root>
  );
};

export const Composer: FC = () => {
  const threadState = useAssistantState((s) => s.thread);
  const threadApi = useAssistantApi();
  const authWarning = usePlaygroundAuthWarning();
  const routes = useRoutes();

  const handleSubmit = () => {
    threadApi.composer().send();
  };

  const handleChange = (value: string) => {
    threadApi.composer().setText(value);
  };

  return (
    <div className="flex flex-col gap-3">
      {authWarning && (
        <div className="flex items-center gap-2 px-3 py-2 bg-warning/10 border border-warning/20 rounded-md text-sm text-warning-foreground">
          <AlertCircle className="size-4 shrink-0" />
          <span>
            {authWarning.missingCount} authentication{" "}
            {authWarning.missingCount === 1 ? "variable" : "variables"} not
            configured.{" "}
            <routes.toolsets.toolset.Link
              params={[authWarning.toolsetSlug]}
              hash="auth"
              className="underline hover:text-foreground font-medium"
            >
              Configure now
            </routes.toolsets.toolset.Link>
          </span>
        </div>
      )}
      <PromptInput onSubmit={handleSubmit}>
        <PromptInputBody>
          <PromptInputTextarea
            placeholder="Send a message..."
            onChange={handleChange}
          />
        </PromptInputBody>
        <PromptInputFooter className="bg-secondary border-t border-neutral-softest rounded-bl-lg rounded-br-lg">
          <PromptInputTools>{/*{additionalActions}*/}</PromptInputTools>
          <PromptInputSubmit
            disabled={threadState.isLoading || threadState.isRunning}
            // status={status}
          />
        </PromptInputFooter>
      </PromptInput>
    </div>
  );
};
