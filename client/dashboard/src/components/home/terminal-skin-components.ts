import { createElement, useContext, type ComponentType, type FC } from "react";
import { ToolFallback } from "@/elements/components/assistant-ui/tool-fallback";
import { ToolGroup } from "@/elements/components/assistant-ui/tool-group";
import { ThreadWelcome } from "@/elements/components/assistant-ui/thread";
import type { ComponentOverrides } from "@/elements/types";
import {
  TerminalToolCall,
  TerminalToolGroup,
  TerminalWelcome,
} from "./terminal-skin";
import { TerminalSkinContext } from "./terminal-skin-theme";

/** Renders the terminal variant inside the terminal, the stock one elsewhere. */
function skinned<P extends object>(
  Terminal: ComponentType<P>,
  Stock: ComponentType<P>,
): FC<P> {
  return function Skinned(props: P) {
    return createElement(
      useContext(TerminalSkinContext) ? Terminal : Stock,
      props,
    );
  };
}

/** Component overrides for the shared runtime. Stock outside the terminal. */
export const TERMINAL_SKIN_COMPONENTS: ComponentOverrides = {
  ToolFallback: skinned(TerminalToolCall, ToolFallback),
  ToolGroup: skinned(TerminalToolGroup, ToolGroup),
  ThreadWelcome: skinned(TerminalWelcome, ThreadWelcome),
};
