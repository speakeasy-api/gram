import { Chat, ElementsConfig, GramElementsProvider } from "@gram-ai/elements";

export function ElementsOnboarding({ config }: { config: ElementsConfig }) {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  );
}
