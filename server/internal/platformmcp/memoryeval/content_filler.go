package memoryeval

import (
	"strconv"
	"strings"
)

// fillerChatCount brings the main project's team history to about 150 chats.
const fillerChatCount = 82

// ticketBase numbers filler tickets so each filler chat has unique text.
const ticketBase = 1200

var fillerComponents = []string{
	"shipments API",
	"tracking page",
	"label service",
	"rate quote service",
	"driver app",
	"billing worker",
	"carrier sync",
	"notifications service",
	"admin dashboard",
	"ETA service",
	"customer portal",
	"webhook dispatcher",
}

// fillerTemplate is routine work that mentions the team's systems without
// deciding anything, so search has realistic noise to sort through.
type fillerTemplate struct {
	title string
	turns [4]string
}

var fillerTemplates = []fillerTemplate{
	{
		title: "Flaky test in the {component}",
		turns: [4]string{
			"{ticket}: the integration test for the {component} fails about one run in ten on CI. Can you look?",
			"The test depends on wall-clock ordering between two inserts. Freezing the clock in the fixture makes it deterministic.",
			"That fixed it locally, 200 runs green.",
			"I'll push the fix and link it on {ticket}.",
		},
	},
	{
		title: "Review PR for the {component}",
		turns: [4]string{
			"Can you review my PR for {ticket}? It refactors the {component} handlers into smaller functions.",
			"Looks good overall. Two handlers lost their error wrapping, and one test now asserts on log output instead of behavior.",
			"Fixed both, ready for another look.",
			"Approved. Merge when CI is green.",
		},
	},
	{
		title: "Slow query in the {component}",
		turns: [4]string{
			"{ticket}: a query in the {component} shows up in the slow query log at around 800ms.",
			"It's filtering on organization and status without a composite index. Adding one on (organization_id, status, created_at) should help.",
			"Added the index in staging and the query is at 12ms.",
			"Ship it with the next migration.",
		},
	},
	{
		title: "Logging cleanup in the {component}",
		turns: [4]string{
			"The {component} logs are too noisy to debug anything. {ticket} tracks cleaning them up.",
			"Most of the noise is info logs inside a loop. Moving them to debug and adding request IDs to the remaining errors would help most.",
			"Done. Log volume from the {component} dropped by about 70%.",
			"Nice. Close {ticket}.",
		},
	},
	{
		title: "Dependency bumps for the {component}",
		turns: [4]string{
			"Dependabot opened a stack of bumps for the {component}. Anything risky? Tracking in {ticket}.",
			"Only the HTTP client bump has breaking changes, in how timeouts are configured. The rest are patch releases.",
			"Updated the timeout config and merged everything.",
			"Good, I'll watch error rates after the deploy.",
		},
	},
	{
		title: "Runbook for the {component}",
		turns: [4]string{
			"We don't have a runbook for the {component}. Can you help draft one? It's {ticket}.",
			"Sections: what it does, dashboards, common alerts and their fixes, how to restart safely, and who owns it.",
			"Drafted it with the three alerts that fired most last quarter.",
			"Link it from the alert descriptions so on-call finds it.",
		},
	},
	{
		title: "Intermittent 500s in the {component}",
		turns: [4]string{
			"Seeing intermittent 500s from the {component} since this morning. {ticket}.",
			"The errors line up with a downstream timeout. The client retries without jitter, so retries pile up together.",
			"Added jitter and the error rate is back to normal.",
			"I'll add a dashboard panel for downstream latency.",
		},
	},
}

// fillerChats generates routine team sessions spread across the 60 days.
func fillerChats() []chatSpec {
	specs := make([]chatSpec, 0, fillerChatCount)
	for i := range fillerChatCount {
		component := fillerComponents[i%len(fillerComponents)]
		template := fillerTemplates[(i/len(fillerComponents)+i)%len(fillerTemplates)]
		ticket := "LUM-" + strconv.Itoa(ticketBase+i)
		fill := strings.NewReplacer("{component}", component, "{ticket}", ticket)
		turns := make([]string, len(template.turns))
		for j, turn := range template.turns {
			turns[j] = fill.Replace(turn)
		}
		owner := users[i%len(users)].Handle
		daysAgo := int(fnv32("filler-day-"+strconv.Itoa(i)) % corpusDays)
		specs = append(specs, team("filler-"+strconv.Itoa(i), owner, daysAgo, fill.Replace(template.title), turns...))
	}
	return specs
}
