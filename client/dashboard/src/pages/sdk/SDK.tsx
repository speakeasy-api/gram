import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Combobox } from "@/components/ui/Combobox";
import { SkeletonCode } from "@/components/ui/Skeleton";
import { TextArea } from "@/components/ui/Textarea";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { capitalize } from "@/lib/utils";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { Stack } from "@/components/ui/Stack";
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
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>SDKs</Page.Section.Title>
          <Page.Section.Description>
            Generate client code that calls your toolsets from the language and
            framework of your choice.
          </Page.Section.Description>
          <Page.Section.Body>
            <AgentifyProvider>
              <SdkContent />
            </AgentifyProvider>
          </Page.Section.Body>
        </Page.Section>
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
      <Text variant="small">{framework}</Text>
    </Combobox>
  );

  const langFrameworkDropdowns = (
    <div className="flex gap-2">
      <SdkLanguageDropdown lang={lang} setLang={handleLanguageChange} />
      {frameworkDropdown}
    </div>
  );

  let heading = (
    <div className="flex items-end justify-between gap-4">
      <Text variant="subheading">
        Use platform MCP servers to build agentic workflows in many popular
        frameworks
      </Text>
      {langFrameworkDropdowns}
    </div>
  );

  if (prompt) {
    heading = (
      <Stack gap={1}>
        <Text variant="subheading">
          What should the agent do?{" "}
          <span className="text-muted-foreground text-sm italic">
            Chat history will also be included in the prompt.
          </span>
        </Text>
        <Stack direction="horizontal" gap={4} align="end">
          <TextArea
            value={prompt}
            onChange={(value) => setPrompt(value)}
            disabled={inProgress}
            placeholder="Look up the weather in San Francisco"
            className="h-20"
          />
          <Stack gap={2}>
            {langFrameworkDropdowns}
            <Button
              onClick={() => {
                void agentify(toolset, environment);
              }}
              variant={outdated || inProgress ? "primary" : "secondary"}
              disabled={inProgress}
            >
              <Button.LeftIcon>
                <Icon name="wand-sparkles" className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Regenerate</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </Stack>
    );
  }

  return (
    <Stack gap={2}>
      {heading}
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
    </Stack>
  );
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
      <Text variant="small" className="capitalize">
        {lang}
      </Text>
    </Combobox>
  );
};
