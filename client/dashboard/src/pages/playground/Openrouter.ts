import { useProject, useSession } from "@/contexts/Auth";
import { getPlaygroundMcpBaseURL } from "@/lib/utils";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";

export const useMiniModel = (): ReturnType<typeof useModel> => {
  return useModel("openai/gpt-5.4-mini");
};

export const useModel = (
  model: string,
  additionalHeaders?: Record<string, string>,
): ReturnType<ReturnType<typeof createOpenRouter>["chat"]> => {
  const session = useSession();
  const project = useProject();

  const openrouter = createOpenRouter({
    apiKey: "this is required",
    baseURL: getPlaygroundMcpBaseURL(),
    headers: {
      "Gram-Session": session.session,
      "Gram-Project": project.slug,
      ...additionalHeaders,
    },
  });

  return openrouter.chat(model);
};
