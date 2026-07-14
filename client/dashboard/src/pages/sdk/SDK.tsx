import { Page } from "@/components/page-layout";
import { WorkbenchLayout } from "@/components/layouts/workbench-layout";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { SkeletonCode } from "@/components/ui/skeleton";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { capitalize } from "@/lib/utils";
import { Stack } from "@/components/ui/stack";
import { CodeSnippet } from "@/components/ui/code-snippet";
import { WandSparkles } from "lucide-react";
import { AgentifyProvider } from "../playground/Agentify";
import { useAgentify } from "../playground/useAgentify";
import {
  CODE_SAMPLES,
  FRAMEWORKS,
  SdkFramework,
  SdkLanguage,
} from "./examples";

export default function SDK(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth fullHeight noPadding>
        <WorkbenchLayout>
          <WorkbenchLayout.Header
            eyebrow="SDKs"
            title="SDKs"
            subtitle="Generate client code that calls your toolsets from the language and framework of your choice."
          />
          <AgentifyProvider>
            <SdkContent />
          </AgentifyProvider>
        </WorkbenchLayout>
      </Page.Body>
    </Page>
  );
}

const SdkContent = ({
  projectSlug,
  toolset = "my-toolset",
  environment = "default",
}: {
  projectSlug?: string;
  toolset?: string;
  environment?: string;
}) => {
  const {
    lang,
    setLang,
    framework,
    setFramework,
    result,
    prompt,
    setPrompt,
    inProgress,
    agentify,
    outdated,
    resultLang,
  } = useAgentify();
  const project = useProject();

  const getCodeSample = () => {
    return (
      result ??
      CODE_SAMPLES[lang][framework as keyof (typeof CODE_SAMPLES)[typeof lang]](
        projectSlug ?? project.slug,
        toolset,
        environment,
      )
    );
  };

  const handleLanguageChange = (newLanguage: SdkLanguage) => {
    setLang(newLanguage);
    // If the current framework exists in the new language, keep it
    if (FRAMEWORKS[newLanguage].some((f) => f === framework)) {
      return;
    }

    setFramework(FRAMEWORKS[newLanguage][0]);
  };

  const frameworkDropdownItems =
    FRAMEWORKS[lang].map((fw) => ({
      label: fw,
      value: fw,
    })) ?? [];

  const frameworkDropdown = (
    <Combobox
      items={frameworkDropdownItems}
      selected={framework}
      onSelectionChange={(value) => setFramework(value.value as SdkFramework)}
      className="max-w-fit"
    >
      <Type variant="small">{framework}</Type>
    </Combobox>
  );

  const langFrameworkDropdowns = (
    <div className="flex gap-2">
      <SdkLanguageDropdown lang={lang} setLang={handleLanguageChange} />
      {frameworkDropdown}
    </div>
  );

  const configPanel = (
    <Stack gap={4} className="p-6">
      <Type variant="subheading">
        Use platform MCP servers to build agentic workflows in many popular
        frameworks
      </Type>
      {langFrameworkDropdowns}
      {prompt !== undefined && (
        <Stack gap={1}>
          <Type variant="subheading">
            What should the agent do?{" "}
            <span className="text-muted-foreground text-sm italic">
              Chat history will also be included in the prompt.
            </span>
          </Type>
          <TextArea
            value={prompt}
            onChange={(value) => setPrompt(value)}
            disabled={inProgress}
            placeholder="Look up the weather in San Francisco"
            className="h-20"
          />
          <Button
            onClick={() => {
              void agentify(toolset, environment);
            }}
            variant={outdated || inProgress ? "primary" : "secondary"}
            disabled={inProgress}
          >
            <Button.LeftIcon>
              <WandSparkles className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Regenerate</Button.Text>
          </Button>
        </Stack>
      )}
    </Stack>
  );

  const previewPanel = (
    <div className="p-6">
      {inProgress ? (
        <SkeletonCode />
      ) : (
        <CodeSnippet
          code={getCodeSample()}
          language={resultLang ?? lang}
          copyable
          fontSize="medium"
          showLineNumbers
          className="border-border"
        />
      )}
    </div>
  );

  return <WorkbenchLayout.Body config={configPanel} preview={previewPanel} />;
};

const SdkLanguageDropdown = ({
  lang,
  setLang,
}: {
  lang: SdkLanguage;
  setLang: (lang: SdkLanguage) => void;
}) => {
  const languageDropdownItems =
    Object.keys(FRAMEWORKS).map((lang) => ({
      label: capitalize(lang),
      value: lang,
    })) ?? [];

  return (
    <Combobox
      items={languageDropdownItems}
      selected={lang}
      onSelectionChange={(value) => setLang(value.value as SdkLanguage)}
      className="max-w-fit"
    >
      <Type variant="small" className="capitalize">
        {lang}
      </Type>
    </Combobox>
  );
};
