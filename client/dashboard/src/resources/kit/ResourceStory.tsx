import { useState } from "react";
import { ChatBridgeProvider } from "./chat-bridge";
import { ResourceView, type ResourceLocation } from "./ResourceView";
import type { ResourceModule } from "./types";

/**
 * Renders a resource the way the prototype's page panel does, so a story is
 * a faithful preview. Navigation works inside the story; chat actions show
 * what the assistant would be asked.
 */
export function ResourceStory<Row extends object, Rec>({
  module,
  initial,
  width,
}: {
  module: ResourceModule<Row, Rec>;
  initial: ResourceLocation;
  /** Panel width in px, to check narrow layouts. */
  width?: number;
}): JSX.Element {
  const [location, setLocation] = useState<ResourceLocation>(initial);
  const [asked, setAsked] = useState<string | null>(null);

  return (
    <ChatBridgeProvider value={{ available: true, ask: setAsked }}>
      <div className="bg-background min-h-screen p-4">
        <div
          className="bg-card mx-auto border px-4 py-8 sm:px-8"
          style={{ maxWidth: width ?? 900 }}
        >
          <ResourceView
            module={module}
            location={location}
            onNavigate={setLocation}
            onOpenRelated={(resource, id) => {
              if (resource === module.definition.id) {
                setLocation({ view: "record", id });
              }
            }}
          />
        </div>
        {asked && (
          <p
            role="status"
            className="bg-card text-muted-foreground mx-auto mt-4 border px-4 py-3 text-sm"
            style={{ maxWidth: width ?? 900 }}
          >
            Assistant would be asked:{" "}
            <span className="text-foreground">{asked}</span>
          </p>
        )}
      </div>
    </ChatBridgeProvider>
  );
}
