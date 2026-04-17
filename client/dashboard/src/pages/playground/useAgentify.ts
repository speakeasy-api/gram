import { createContext, useContext } from "react";
import { SdkFramework, SdkLanguage, OPENAI_AGENTS_SDK } from "../sdk/examples";

export const AgentifyContext = createContext<{
  lang: SdkLanguage;
  setLang: (lang: SdkLanguage) => void;
  framework: SdkFramework;
  setFramework: (framework: SdkFramework) => void;
  inProgress: boolean;
  prompt: string | undefined;
  setPrompt: (prompt: string | undefined) => void;
  result: string | undefined;
  resultLang: SdkLanguage | undefined;
  outdated: boolean;
  agentify: (toolsetSlug: string, environmentSlug: string) => Promise<string>;
}>({
  lang: "python",
  setLang: () => {},
  framework: OPENAI_AGENTS_SDK,
  setFramework: () => {},
  inProgress: false,
  prompt: undefined,
  setPrompt: () => {},
  result: undefined,
  resultLang: undefined,
  outdated: false,
  agentify: () => Promise.resolve(""),
});

export const useAgentify = () => {
  return useContext(AgentifyContext);
};
