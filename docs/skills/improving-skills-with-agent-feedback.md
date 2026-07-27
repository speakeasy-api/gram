# Improving skills with agent feedback

Gram can use feedback reported by agents and sampled efficacy scores to propose changes to a skill. The review workflow keeps those proposals separate from the current `SKILL.md` until a project member approves them.

## What agents report

After using a skill, an agent can report whether it helped, partially helped, did not help, was misleading, or was harmful. A report can include a short note and the skill version used in the session.

These reports are raw, agent-reported signals. They are inputs to analysis, not authoritative measurements of a skill's efficacy. Treat outcome counts and notes as context alongside sampled efficacy scores, session rationale, and version trends.

## How analysis works

An automated analysis agent reviews eligible feedback and scored sessions for a skill. When the evidence supports a concrete improvement, it records the edit as a unified diff against the skill version it analyzed, along with:

- A short summary of what goes wrong today and what the edit fixes.
- A link to every feedback record the proposal was generated from, which is where the reported feedback and session counts come from.
- How many scored sessions informed the proposal.
- The exact skill version used as the proposal's base.

The analysis agent does not update the skill directly. The proposal remains an open suggestion until someone reviews it.

A suggestion is a list of separate changes rather than one rewritten manifest. The agent is asked to keep each change self-contained and to cite only the feedback behind that change, so a reviewer reading a change sees the reports that motivated it and not the ones behind an unrelated edit. Each change stores its own diff, so it survives later edits it does not overlap: on each analysis pass Gram replays every open change onto the current version, carrying forward the ones that still apply and dropping the ones that conflict or are already applied. A suggestion is superseded once nothing is left to propose.

## Review a suggested edit

The skill detail page shows a suggested edit as a diff between the current and proposed manifests, with a review marker beside each proposed change. Expanding a marker shows that change's summary, how many sessions asked for it, and an expander for the agent reports cited as its reason. Project members with skill write access can:

- **Apply** to take just that change. Gram records a new immutable version carrying only it, and the suggestion stays open proposing the remaining changes, now measured against the version you just created. Applying the last remaining change closes the suggestion.
- **Apply all** to review every change the suggestion still proposes and take them as one new version. From there you can also adjust the complete proposed manifest before applying it, or dismiss the suggestion without changing the skill. The normal manifest validation and 65,536-byte UTF-8 limit still apply.

There is no draft state. Every apply records a new version immediately, and that version becomes the one agents load, so plugin distributions that are not pinned to a specific version pick it up.

Approval applies the change to the version that is current at that moment. If the change no longer applies, Gram reports a conflict or supersedes the suggestion rather than applying it over newer work.

After every suggestion page has loaded, the skills list can approve the exact set shown in the confirmation dialog. Suggestions created after the dialog opens are not included. There is no dashboard batch limit. Bulk approval processes each confirmed suggestion independently. A selected suggestion that is no longer open is reported as a conflict. The API returns applied, superseded, conflicting, and failed item outcomes. The dashboard also computes a skipped count as the number of confirmed IDs absent from the response, which can happen when an ID is missing or archived as the snapshot is processed. Review the reported counts instead of assuming every suggestion was applied. If the request fails, refresh and review the current state before retrying because some edits may already have been applied.

## Read feedback and regression signals

The collapsed **All agent reviews** section at the bottom of a skill shows all-time outcome counts and recent notes across every report, not only the ones behind the current suggestion. It intentionally exposes only the privacy-minimized feedback fields returned by the API. It does not appear on the primary skills list.

Skill insights may also show a regression warning when the server's comparison policy identifies the current version as a regression. The warning includes current and predecessor scores and sample counts and links to the predecessor in version history. The dashboard does not calculate its own threshold.

## Restore an earlier version

Version history can restore any valid, non-current version. Restoring makes that historical content current again without changing the immutable historical record. Versions are content-addressed, so restore reactivates the existing version rather than creating a duplicate with the same canonical content. It does not rewrite or remove versions.

Explicit distribution pins for plugins and assistants are preserved. A pinned distribution continues to target its selected version; only distributions that follow the current skill version observe the restore.
