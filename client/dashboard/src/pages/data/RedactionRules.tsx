import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { Badge, Button, Column, Icon, Table } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { AddRedactionRuleSheet } from "./AddRedactionRuleSheet";
import {
  buildMockRules,
  TARGET_LABELS,
  type RedactionRule,
} from "./redaction-rules-data";

function scopeLabel(rule: RedactionRule): string {
  return rule.project ?? "All projects";
}

function buildColumns(
  onToggle: (id: string, enabled: boolean) => void,
): Column<RedactionRule>[] {
  return [
    {
      key: "subject",
      header: "Individual",
      width: "220px",
      render: (rule) => (
        <div className="min-w-0">
          <Type small className="block truncate font-medium">
            {rule.subjectName || rule.subjectEmail}
          </Type>
          {rule.subjectName && (
            <Type muted small className="block truncate">
              {rule.subjectEmail}
            </Type>
          )}
        </div>
      ),
    },
    {
      key: "targets",
      header: "Redacts",
      render: (rule) => (
        <div className="flex flex-wrap gap-1">
          {rule.targets.map((target) => (
            <Badge key={target} variant="neutral">
              <Badge.Text>{TARGET_LABELS[target]}</Badge.Text>
            </Badge>
          ))}
        </div>
      ),
    },
    {
      key: "scope",
      header: "Applies To",
      width: "140px",
      render: (rule) => (
        <Type muted small className="font-mono">
          {scopeLabel(rule)}
        </Type>
      ),
    },
    {
      key: "createdAt",
      header: "Added",
      width: "130px",
      render: (rule) => (
        <Type muted small className="whitespace-nowrap">
          <HumanizeDateTime date={rule.createdAt} />
        </Type>
      ),
    },
    {
      key: "enabled",
      header: "Enabled",
      width: "90px",
      render: (rule) => (
        <Switch
          checked={rule.enabled}
          onCheckedChange={(enabled) => onToggle(rule.id, enabled)}
          aria-label={`Toggle redaction rule for ${rule.subjectEmail}`}
        />
      ),
    },
  ];
}

/**
 * Per-individual telemetry redaction rules, rendered as a section of the
 * Data Configuration page. Prototype: local state only, no persistence.
 */
export function RedactionRulesSection(): JSX.Element {
  const [rules, setRules] = useState(() => buildMockRules());
  const [addOpen, setAddOpen] = useState(false);

  const toggleRule = (id: string, enabled: boolean) => {
    setRules((prev) =>
      prev.map((rule) => (rule.id === id ? { ...rule, enabled } : rule)),
    );
  };

  const columns = buildColumns(toggleRule);

  return (
    <div>
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="mb-2 flex items-center gap-2">
            <Heading variant="h4">Redaction Rules</Heading>
            <ReleaseStageBadge stage="preview" />
          </div>
          <Type muted small className="mb-6 block">
            Strip or mask sensitive fields from telemetry for specific
            individuals before events are stored. Rules apply at ingest across
            every project in your organization and are not retroactive.
          </Type>
        </div>
        <Button size="sm" onClick={() => setAddOpen(true)}>
          <Button.LeftIcon>
            <Icon name="plus" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Add Rule</Button.Text>
        </Button>
      </div>
      <Table
        columns={columns}
        data={rules}
        rowKey={(rule) => rule.id}
        noResultsMessage={<Type muted>No redaction rules configured</Type>}
      />
      <AddRedactionRuleSheet
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onAdd={(rule) => setRules((prev) => [rule, ...prev])}
      />
    </div>
  );
}
