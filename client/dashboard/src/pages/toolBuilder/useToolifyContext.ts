import { ToolsetEntry } from "@gram/client/models/components";
import { createContext, useContext } from "react";
import { z } from "zod";

export const SuggestionSchema = z.object({
  name: z.string(),
  description: z.string(),
  inputs: z.array(
    z.object({
      name: z.string(),
      description: z.string(),
    }),
  ),
  steps: z.array(
    z.object({
      tool: z.string(),
      instructions: z.string(),
    }),
  ),
});

export type ToolifyContextType = {
  toolset: ToolsetEntry;
  purpose: string;
  suggestion: z.infer<typeof SuggestionSchema>;
};

//eslint-disable-next-line @typescript-eslint/no-explicit-any
export const emptyCtx: ToolifyContextType = {} as any;

export const ToolifyContext = createContext<
  ToolifyContextType & { set: (t: ToolifyContextType) => void }
>({ ...emptyCtx, set: () => {} });

export const useToolifyContext = () => {
  return useContext(ToolifyContext);
};
