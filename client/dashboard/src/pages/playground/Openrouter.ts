import { useProject, useSession } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";

export const useMiniModel = () => {
  return useModel("openai/gpt-4o-mini");
};

export const useModel = (
  model: string,
  additionalHeaders?: Record<string, string>,
) => {
  const session = useSession();
  const project = useProject();

  const openrouter = createOpenRouter({
    apiKey: "this is required",
    baseURL: getServerURL(),
    headers: {
      "Gram-Session": session.session,
      "Gram-Project": project.slug,
      ...additionalHeaders,
    },
  });

  return openrouter.chat(model);
};
