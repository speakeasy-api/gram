# Improving skills with agent feedback

Gram can use feedback reported by agents and sampled efficacy scores to propose changes to a skill. The review workflow keeps those proposals separate from the current `SKILL.md` until a project member approves them.

## What agents report

After using a skill, an agent can report whether it helped, partially helped, did not help, was misleading, or was harmful. A report can include a short note and the skill version used in the session.

These reports are raw, agent-reported signals. They are inputs to analysis, not authoritative measurements of a skill's efficacy. Treat outcome counts and notes as context alongside sampled efficacy scores, session rationale, and version trends.

## How analysis works

An automated analysis agent reviews eligible feedback and scored sessions for a skill. When the evidence supports a concrete improvement, it creates one complete proposed `SKILL.md` and records:

- Why the edit was proposed.
- How many feedback records informed the proposal.
- How many scored sessions informed the proposal.
- The exact skill version used as the proposal's base.

The analysis agent does not update the skill directly. The proposed content remains an open suggestion until someone reviews it.

## Review a suggested edit

The skill detail page shows the current and proposed manifests as a diff. Project members with skill write access can:

- **Approve** to apply the proposed content as a new immutable version.
- **Edit and approve** to adjust the complete proposed manifest before applying it. The normal manifest validation and 65,536-byte UTF-8 limit still apply.
- **Dismiss** to close the suggestion without changing the skill.

A suggestion is tied to its base version. If the current skill changes before approval, Gram marks the stale suggestion as superseded rather than applying it over newer work.

After every suggestion page has loaded, the skills list can approve the exact set shown in the confirmation dialog. Suggestions created after the dialog opens are not included. There is no dashboard batch limit. Bulk approval processes each confirmed suggestion independently. A selected suggestion that is no longer open is reported as a conflict. The API returns applied, superseded, conflicting, and failed item outcomes. The dashboard also computes a skipped count as the number of confirmed IDs absent from the response, which can happen when an ID is missing or archived as the snapshot is processed. Review the reported counts instead of assuming every suggestion was applied. If the request fails, refresh and review the current state before retrying because some edits may already have been applied.

## Read feedback and regression signals

The collapsed feedback log on a skill shows all-time outcome counts and recent notes. It intentionally exposes only the privacy-minimized feedback fields returned by the API. It does not appear on the primary skills list.

Skill insights may also show a regression warning when the server's comparison policy identifies the current version as a regression. The warning includes current and predecessor scores and sample counts and links to the predecessor in version history. The dashboard does not calculate its own threshold.

## Restore an earlier version

Version history can restore any valid, non-current version. Restoring makes that historical content current again without changing the immutable historical record. It does not rewrite or remove versions.

Explicit distribution pins for plugins and assistants are preserved. A pinned distribution continues to target its selected version; only distributions that follow the current skill version observe the restore.
