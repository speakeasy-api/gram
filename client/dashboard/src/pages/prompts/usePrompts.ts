import { isPrompt } from "@/lib/toolTypes";
import { useTemplates } from "@gram/client/react-query/index.js";

export function usePrompts() {
  const { data, isLoading } = useTemplates();
  return {
    prompts: data?.templates.filter(isPrompt),
    isLoading,
  };
}
