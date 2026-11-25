import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { useEffect, useRef, useState } from "react";
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
  onStart,
  onDone,
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
  /** Called when a run starts. */
  onStart?: () => void;
  /** Called once a run settles without an error. */
  onDone?: () => void;
}): JSX.Element | null {
  const [confirming, setConfirming] = useState(false);
  const bridge = useChatBridge();
  const state = handlers[def.id];
  const pending = state?.pending ?? false;
  const failed = state?.failed ?? false;
  // Set when this button starts a run and sees it go pending. React Query
  // reports pending a render after `run`, so "not pending" right after a
  // click does not mean the run has settled.
  const running = useRef<"idle" | "started" | "pending">("idle");

  // The confirm dialog stays open and locked while the action runs, then
  // closes when it settles.
  useEffect(() => {
    if (pending && running.current === "started") {
      running.current = "pending";
    } else if (!pending && running.current === "pending") {
      running.current = "idle";
      setConfirming(false);
      if (!failed) onDone?.();
    }
  }, [pending, failed, onDone]);

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

  // Danger-zone buttons always ask first, even for an action declared
  // without its own dialog.
  const confirm =
    def.confirm ??
    (emphasis === "danger"
      ? {
          title: `${def.label}?`,
          description: "This can't be undone.",
          confirmLabel: def.label,
          pendingLabel: "Working…",
        }
      : undefined);

  const start = () => {
    running.current = "started";
    onStart?.();
    state.run(input);
  };

  const onClick = () => {
    if (def.kind === "chat") {
      bridge.ask(fillPrompt(def.prompt ?? def.label, subject ?? ""));
    } else if (confirm) {
      setConfirming(true);
    } else {
      start();
    }
  };

  const button = (
    <Button
      variant={variant}
      size={size}
      onClick={onClick}
      disabled={state.pending || state.disabledReason != null}
    >
      {state.pending && confirm ? confirm.pendingLabel : (label ?? def.label)}
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
      {confirm && (
        <ConfirmDialog
          open={confirming}
          onOpenChange={setConfirming}
          title={confirm.title}
          description={confirm.description}
          confirmLabel={confirm.confirmLabel}
          pendingLabel={confirm.pendingLabel}
          confirmVariant={
            def.danger || emphasis === "danger"
              ? "destructive-primary"
              : "primary"
          }
          isPending={state.pending}
          onConfirm={start}
        />
      )}
    </>
  );
}
