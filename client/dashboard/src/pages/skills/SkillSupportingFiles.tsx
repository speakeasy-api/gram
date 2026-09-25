import { Badge } from "@/components/ui/Badge";
import { Column, Table } from "@/components/ui/Table";
import { SettingsSection } from "@/components/detail/settings-section";
import type { SkillResourceReference } from "@gram/client/models/components/skillresourcereference.js";
import { Text } from "@/components/ui/Text";
import type { BadgeVariant } from "@/components/ui/lib/types";

const KIND_LABELS: Record<string, string> = {
  script: "Script",
  reference: "Reference",
  asset: "Asset",
  other: "Other",
};

function kindLabel(kind: string): string {
  return KIND_LABELS[kind] ?? KIND_LABELS.other!;
}

// Executable content is the part of a skill a security reviewer cares about
// most, so scripts are the only kind that reads as a caution.
function kindVariant(kind: string): BadgeVariant {
  return kind === "script" ? "warning" : "neutral";
}

const columns: Column<SkillResourceReference>[] = [
  {
    key: "path",
    header: "Path",
    render: (reference) => (
      <Text small className="font-mono break-all">
        {reference.path}
      </Text>
    ),
  },
  {
    key: "kind",
    header: "Kind",
    width: "120px",
    render: (reference) => (
      <Badge variant={kindVariant(reference.kind)} size="sm" background>
        {kindLabel(reference.kind)}
      </Badge>
    ),
  },
];

export function SkillSupportingFiles({
  references,
}: {
  references: SkillResourceReference[];
}): JSX.Element | null {
  if (references.length === 0) {
    return null;
  }

  const scriptCount = references.filter(
    (reference) => reference.kind === "script",
  ).length;

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Supporting files</SettingsSection.Title>
        <SettingsSection.Description>
          Files this manifest points at, relative to the skill directory root.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Table
            columns={columns}
            data={references}
            rowKey={(reference) => reference.path}
          />
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            <SupportingFilesHint scriptCount={scriptCount} />
          </SettingsSection.FooterHint>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function SupportingFilesHint({
  scriptCount,
}: {
  scriptCount: number;
}): JSX.Element {
  if (scriptCount === 0) {
    return (
      <Text small muted>
        Gram stores a skill as a single SKILL.md, so these files are not
        captured here and are not included when the skill is distributed.
      </Text>
    );
  }

  return (
    <Text small muted>
      Gram stores a skill as a single SKILL.md, so these files are not captured
      here and are not included when the skill is distributed. This manifest
      runs executable content Gram cannot show you for review.
    </Text>
  );
}
