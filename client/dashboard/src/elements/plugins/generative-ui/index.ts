import { Plugin } from "@/elements/types/plugins";
import generativeUIPrompt from "@/elements/prompts/generative-ui.txt?raw";
import { GenerativeUIRenderer } from "./component";

/**
 * This plugin renders json-render UI trees as dynamic widgets.
 * Use the language identifier 'ui' or 'json-render' in code blocks.
 */
export const generativeUI: Plugin = {
  id: "generative-ui",
  language: "ui",
  prompt: generativeUIPrompt,
  Component: GenerativeUIRenderer,
  Header: undefined,
};
