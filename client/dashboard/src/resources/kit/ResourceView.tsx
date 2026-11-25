import { ResourceRecord } from "./ResourceRecord";
import { ResourceTable } from "./ResourceTable";
import type { ResourceModule } from "./types";

export type ResourceLocation =
  | { view: "list" }
  | { view: "record"; id: string };

/** A resource's mini app: its list and its record, switched by location. */
export function ResourceView<Row extends object, Rec>({
  module,
  location,
  onNavigate,
  onOpenRelated,
  hideBackLink,
}: {
  module: ResourceModule<Row, Rec>;
  location: ResourceLocation;
  onNavigate: (location: ResourceLocation) => void;
  onOpenRelated?: (resource: string, id: string) => void;
  hideBackLink?: boolean;
}): JSX.Element {
  if (location.view === "record") {
    return (
      <ResourceRecord
        // Remount per record so local drafts never leak between records.
        key={location.id}
        module={module}
        id={location.id}
        onBack={() => onNavigate({ view: "list" })}
        onOpenRelated={onOpenRelated}
        hideBackLink={hideBackLink}
      />
    );
  }
  return (
    <ResourceTable
      module={module}
      onOpen={(id) => onNavigate({ view: "record", id })}
    />
  );
}
