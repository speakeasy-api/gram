import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { SettingsSection } from "@/pages/mcp/x/tabs/settings/SettingsSection";
import type { GetSkillResult } from "@gram/client/models/components/getskillresult.js";
import type { SkillSightingTimelinePoint } from "@gram/client/models/components/skillsightingtimelinepoint.js";
import { type Column, Table } from "@speakeasy-api/moonshine";

export const SKILL_ADOPTION_SECTION_ID = "adoption";
export const SKILL_TIMELINE_SECTION_ID = "timeline";

const utcMonthDayFormatter = new Intl.DateTimeFormat(undefined, {
  month: "long",
  day: "numeric",
  timeZone: "UTC",
});

function metricValue(value: number): string {
  return new Intl.NumberFormat().format(value);
}

function machineLabel(count: number): string {
  return count === 1 ? "machine" : "machines";
}

export function SkillActivitySections({
  data,
}: {
  data: GetSkillResult;
}): JSX.Element {
  const { skill, adoption, drift, sightingTimeline } = data;
  const timelineColumns: Column<SkillSightingTimelinePoint>[] = [
    {
      key: "day",
      header: "Day",
      render: (point) => utcMonthDayFormatter.format(point.bucketStart),
    },
    {
      key: "activations",
      header: "Activations",
      width: "140px",
      render: (point) => metricValue(point.activationCount),
    },
  ];

  return (
    <>
      <SettingsSection id={SKILL_ADOPTION_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Adoption and drift</SettingsSection.Title>
          <SettingsSection.Description>
            Activation coverage and version convergence over the last 30 days.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <dl className="grid gap-px overflow-hidden rounded-lg border sm:grid-cols-2 lg:grid-cols-4">
              <Metric
                label="Versions"
                value={metricValue(skill.versionCount)}
              />
              <Metric
                label="Active machines"
                value={metricValue(adoption.distinctHostnames)}
              />
              <Metric
                label="30-day activations"
                value={metricValue(adoption.activationsInWindow)}
              />
              <Metric
                label="Drifted"
                value={metricValue(drift.driftedMachines)}
              />
            </dl>
            <Type small muted>
              {drift.targetState === "single" && (
                <>
                  {metricValue(drift.onTargetMachines)}{" "}
                  {machineLabel(drift.onTargetMachines)}{" "}
                  {drift.onTargetMachines === 1 ? "is" : "are"} on the
                  distributed version.{" "}
                  {metricValue(drift.indeterminateMachines)}{" "}
                  {machineLabel(drift.indeterminateMachines)}{" "}
                  {drift.indeterminateMachines === 1 ? "has" : "have"} an
                  unknown version.
                </>
              )}
              {drift.targetState === "not_distributed" &&
                "No plugin distribution target is configured, so drift is indeterminate."}
              {drift.targetState === "ambiguous" &&
                "Multiple plugin distribution targets are configured, so drift is indeterminate."}
            </Type>
          </SettingsSection.Body>
          {(skill.firstSeenAt || skill.lastSeenAt) && (
            <SettingsSection.Footer>
              <SettingsSection.FooterHint>
                {skill.firstSeenAt && (
                  <>
                    First activated{" "}
                    <HumanizeDateTime date={skill.firstSeenAt} />
                  </>
                )}
                {skill.firstSeenAt && skill.lastSeenAt && " · "}
                {skill.lastSeenAt && (
                  <>
                    Last activated <HumanizeDateTime date={skill.lastSeenAt} />
                  </>
                )}
              </SettingsSection.FooterHint>
            </SettingsSection.Footer>
          )}
        </SettingsSection.Panel>
      </SettingsSection>

      <SettingsSection id={SKILL_TIMELINE_SECTION_ID}>
        <SettingsSection.Header>
          <SettingsSection.Title>Activation timeline</SettingsSection.Title>
          <SettingsSection.Description>
            Daily activation volume for the rolling 30-day window.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            {sightingTimeline.length === 0 ? (
              <Type small muted>
                No activations captured in the last 30 days.
              </Type>
            ) : (
              <Table
                columns={timelineColumns}
                data={sightingTimeline}
                rowKey={(point) => point.bucketStart.toISOString()}
              />
            )}
          </SettingsSection.Body>
        </SettingsSection.Panel>
      </SettingsSection>
    </>
  );
}

function Metric({
  label,
  value,
}: {
  label: string;
  value: string;
}): JSX.Element {
  return (
    <div className="bg-card px-4 py-3">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="mt-1 text-xl font-semibold tabular-nums">{value}</dd>
    </div>
  );
}
