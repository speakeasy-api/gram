import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { useEffect, useState } from "react";
import { fillPrompt, useChatBridge } from "./chat-bridge";
import type { ActionDef, ActionHandlers } from "./types";

type Emphasis = "primary" | "secondary" | "tertiary" | "danger";

/**
 * One action, drawn the same way everywhere. Hidden when the data layer gives
 * no handler (the user lacks the scope); disabled with its reason on hover
 * when the handler says why it cannot run.
 */
export function ActionButton({
  def,
  handlers,
  emphasis,
  input,
  subject,
  label,
  size = "md",
}: {
  def: ActionDef;
  handlers: ActionHandlers;
  emphasis: Emphasis;
  /** Passed to the handler (an item id, a new value). */
  input?: string;
  /** The record's name, for `{title}` in chat prompts. */
  subject?: string;
  /** Overrides the action's label where context already names it. */
  label?: string;
  size?: "sm" | "md";
}): JSX.Element | null {
  const [confirming, setConfirming] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const bridge = useChatBridge();
  const state = handlers[def.id];
  const pending = state?.pending ?? false;

  // The confirm dialog stays open and locked while the action runs, then
  // closes when it settles.
  useEffect(() => {
    if (submitted && !pending) {
      setSubmitted(false);
      setConfirming(false);
    }
  }, [submitted, pending]);

  if (!state) return null;
  // Chat actions hand the job to the assistant; without one they don't exist.
  if (def.kind === "chat" && !bridge.available) return null;

  const variant =
    emphasis === "danger"
      ? "destructive-primary"
      : emphasis === "primary"
        ? "primary"
        : emphasis === "secondary"
          ? "secondary"
          : "tertiary";

  const onClick = () => {
    if (def.kind === "chat") {
      bridge.ask(fillPrompt(def.prompt ?? def.label, subject ?? ""));
    } else if (def.kind === "confirm" && def.confirm) {
      setConfirming(true);
    } else {
      state.run(input);
    }
  };

  const button = (
    <Button
      variant={variant}
      size={size}
      onClick={onClick}
      disabled={state.pending || state.disabledReason != null}
    >
      {state.pending && def.confirm
        ? def.confirm.pendingLabel
        : (label ?? def.label)}
    </Button>
  );

  return (
    <>
      {state.disabledReason ? (
        <Tooltip>
          <TooltipTrigger asChild>
            {/* A disabled button gets no pointer events; the span holds the hover. */}
            <span tabIndex={0}>{button}</span>
          </TooltipTrigger>
          <TooltipContent>{state.disabledReason}</TooltipContent>
        </Tooltip>
      ) : (
        button
      )}
      {def.confirm && (
        <ConfirmDialog
          open={confirming}
          onOpenChange={setConfirming}
          title={def.confirm.title}
          description={def.confirm.description}
          confirmLabel={def.confirm.confirmLabel}
          pendingLabel={def.confirm.pendingLabel}
          confirmVariant={def.danger ? "destructive-primary" : "primary"}
          isPending={state.pending}
          onConfirm={() => {
            state.run(input);
            setSubmitted(true);
          }}
        />
      )}
    </>
  );
}
