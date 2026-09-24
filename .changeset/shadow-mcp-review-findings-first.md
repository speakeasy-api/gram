---
"dashboard": patch
---

Rework the Shadow MCP server review around the findings rather than the raw evidence. A "What stands out" panel above the questions ranks everything the dossier supports — concerns, notable authority, what could not be established, and which checks ran clean — and each finding links to the section showing its working, so a server declaring dozens of tools no longer hides "27 of them act on your behalf" in a note beside a heading.

Make the tool listing usable at scale: it is ordered by how much authority each tool declares, searchable by name, and filterable to the tools that act on your behalf, declare destructive effects, or declare nothing at all, with a line stating how many of the declared tools the current view shows.

Stop asking questions that cannot be answered and stop repeating alarms. The published-advisories section renders only for a package, since advisory databases do not index hosted endpoints; absences such as an unregistered domain, a missing source repository, or an archived one are stated as facts in the list a reader is already scanning instead of as separate banners; and "No version is pinned" no longer appears for hosted endpoints, which have no version to pin.

Rename the summary tile from "Status" to "Access" so it no longer reads as a second opinion on the review's own state, and reframe the web research section around what a run answers that the server's self-description cannot, hiding it entirely for organizations without the feature and no prior run.

Fix `humanize(date, { includeTime: false })` rendering a clock time for dates in an earlier year, which showed domain registrations as "Jul 5, 2001, 3:41 AM".
