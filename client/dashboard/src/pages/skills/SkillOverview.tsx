import { RequireScope } from "@/components/require-scope";
import { StatusBanner } from "@/components/status-banner";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import type { SkillPromptInjectionFinding } from "@gram/client/models/components/skillpromptinjectionfinding.js";
import { ChevronRight, CircleAlert } from "lucide-react";
import { useState } from "react";
import { EditSkillDetailsDialog } from "./EditSkillDetailsDialog";
import { SkillInsightsSection } from "./SkillInsightsSection";
import { SkillPluginBanner } from "./SkillPluginBanner";
import { useSkillDetailContext } from "./SkillDetailContext";
import { SettingsSection } from "@/components/detail/settings-section";
import { Badge } from "@/components/ui/Badge";

export default function SkillOverview(): JSX.Element {
  const project = useProject();
  const { skillQueryData, versionLabels, versionsLoading, versionsError } =
    useSkillDetailContext();
  const { skill } = skillQueryData;
  const [detailsOpen, setDetailsOpen] = useState(false);

  return (
    <>
      <PromptInjectionBanner
        findings={skillQueryData.promptInjectionFindings}
      />
      <SkillPluginBanner skill={skill} />
      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>Skill details</SettingsSection.Title>
          <SettingsSection.Description>
            Registry identity and presentation metadata.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <dl className="grid gap-4 sm:grid-cols-3">
              <div>
                <dt className="text-muted-foreground text-xs">
                  Canonical name
                </dt>
                <dd className="mt-1 font-mono text-sm">{skill.name}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">Display name</dt>
                <dd className="mt-1 text-sm">{skill.displayName}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground text-xs">Summary</dt>
                <dd className="mt-1 text-sm">{skill.summary || "None"}</dd>
              </div>
              <div className="sm:col-span-3">
                <dt className="text-muted-foreground text-xs">Tags</dt>
                <dd className="mt-1">
                  {skill.tags.length > 0 ? (
                    <div className="flex flex-wrap gap-1.5">
                      {skill.tags.map((tag) => (
                        <Badge key={tag} variant="neutral" className="text-xs">
                          {tag}
                        </Badge>
                      ))}
                    </div>
                  ) : (
                    <span className="text-sm">None</span>
                  )}
                </dd>
              </div>
            </dl>
          </SettingsSection.Body>
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Renaming keeps activation attribution on this skill.
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              <RequireScope
                scope="skill:write"
                resourceId={project.id}
                level="component"
              >
                <Button size="sm" onClick={() => setDetailsOpen(true)}>
                  Edit details
                </Button>
              </RequireScope>
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        </SettingsSection.Panel>
      </SettingsSection>
      <SkillInsightsSection
        data={skillQueryData}
        versionLabels={versionLabels}
        versionsLoading={versionsLoading}
        versionsError={versionsError}
      />
      <EditSkillDetailsDialog
        key={detailsOpen ? "details-open" : "details-closed"}
        skill={skill}
        open={detailsOpen}
        onOpenChange={setDetailsOpen}
      />
    </>
  );
}

function PromptInjectionBanner({
  findings,
}: {
  findings: SkillPromptInjectionFinding[];
}): JSX.Element | null {
  const [expanded, setExpanded] = useState(false);
  if (findings.length === 0) return null;
  const summary =
    findings.length === 1
      ? "A prompt injection finding was detected in the current skill version."
      : `${findings.length} prompt injection findings were detected in the current skill version.`;
  return (
    <StatusBanner tone="destructive">
      <Collapsible open={expanded} onOpenChange={setExpanded}>
        <div className="flex flex-col gap-3 p-6">
          <div className="flex items-center gap-2">
            <CircleAlert className="text-destructive h-4 w-4 shrink-0" />
            <Text className="text-destructive text-base font-semibold">
              Prompt injection flagged
            </Text>
          </div>
          <Text variant="small" className="text-muted-foreground/90">
            {summary}
          </Text>
          <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex w-fit items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
            <ChevronRight className="h-4 w-4 transition-transform" />
            {expanded ? "Hide findings" : "Show findings"}
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className="divide-border divide-y">
              {findings.map((finding) => (
                <li
                  key={`${finding.ruleId}:${finding.description}:${finding.confidence}`}
                  className="space-y-1 py-3 first:pt-0 last:pb-0"
                >
                  <div className="flex flex-wrap items-baseline gap-x-3">
                    <span className="font-mono text-sm">{finding.ruleId}</span>
                    <Text small muted>
                      {Math.round(finding.confidence * 100)}% confidence
                    </Text>
                  </div>
                  <Text small>{finding.description}</Text>
                </li>
              ))}
            </ul>
          </CollapsibleContent>
        </div>
      </Collapsible>
    </StatusBanner>
  );
}
