import { isPrompt } from "@/lib/toolTypes";
import { useTemplates } from "@gram/client/react-query/index.js";

export function usePrompts(): {
  prompts:
    | NonNullable<ReturnType<typeof useTemplates>["data"]>["templates"]
    | undefined;
  isLoading: boolean;
} {
  const { data, isLoading } = useTemplates();
  return {
    prompts: data?.templates.filter(isPrompt),
    isLoading,
  };
}
