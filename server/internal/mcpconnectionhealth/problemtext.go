package mcpconnectionhealth

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
)

// headlines are the headline per rule, for a sign-in to Speakeasy and for the rest.
var headlines = map[problems.Rule][2]string{
	problems.RuleSameServer:     {"{people} can't sign in to {scope}", "{people} can't connect to {scope}"},
	problems.RuleSameService:    {"{people} can't sign in to {scope}", "{people} can't connect to {scope}"},
	problems.RuleSameAppVersion: {"{people} on {scope} can't sign in", "{people} on {scope} can't connect"},
	problems.RuleOnePerson:      {"{scope} can't sign in", "{scope} can't connect"},
}

// The fact sentences. Each states only what the episodes establish.
const (
	factOn             = "On {servers}."
	factAppSignIn      = "The app's sign-in to Speakeasy fails."
	factServiceSignIn  = "Signing in to {service} fails."
	factSavedSignIn    = "The saved sign-in for {service} is not accepted."
	factNotAnswering   = "{server} is not answering."
	factNotAnsweringUs = "{server} does not answer these people."
	factRequestsFail   = "Requests to {server} are failing."
	factAllUse         = "All use {app}."
	factUses           = "Uses {app}."
	factOtherApps      = "Other apps worked there in this time."
)

// savedCredentialReasons fail a saved key or shared credential: nobody signs in.
var savedCredentialReasons = []string{
	mcpconnectionattempts.CodeStaticKeyRejected.String(),
	mcpconnectionattempts.CodeSharedCredentialRejected.String(),
	mcpconnectionattempts.CodeCredentialMisconfigured.String(),
}

func fill(template string, values ...string) string {
	return strings.NewReplacer(values...).Replace(template)
}

func headline(p problems.Problem, scope string) string {
	part := 1
	if p.Part == mcpconnectionattempts.PartAppSignIn {
		part = 0
	}
	return fill(headlines[p.Rule][part], "{people}", fmt.Sprintf("%d people", p.People), "{scope}", scope)
}

// fact states what failed in plain words, using the catalog's own sentences.
func fact(n names, p problems.Problem) string {
	var out []string
	servers := problemServers(p)
	if p.Rule == problems.RuleOnePerson || p.Rule == problems.RuleSameAppVersion {
		if where := serversPhrase(n, p); where != "" {
			out = append(out, fill(factOn, "{servers}", where))
		}
	}
	service, server := "the service", "The server"
	if p.Rule == problems.RuleSameService {
		service = hostOf(p.Scope.Key)
		server = "The service at " + service
	}
	reason := sharedReason(p)
	meaning, _ := mcpconnectionattempts.MeaningOf(reason)
	// The catalog's administrator sentence is about one attempt on one server.
	adminSentence := ""
	if reason != "" && servers == 1 {
		adminSentence = meaning.WhatHappenedForAdmin
	}
	switch {
	case p.Part == mcpconnectionattempts.PartAppSignIn:
		out = append(out, factAppSignIn)
	case allSavedCredential(p):
		switch {
		case adminSentence != "":
			out = append(out, adminSentence)
			adminSentence = ""
		case reason != "" && meaning.WhatHappened != "":
			out = append(out, meaning.WhatHappened)
		default:
			out = append(out, fill(factSavedSignIn, "{service}", service))
		}
	case p.Part == mcpconnectionattempts.PartServiceSignIn:
		out = append(out, fill(factServiceSignIn, "{service}", service))
	case p.WhoFixes != mcpconnectionattempts.WhoProvider:
		out = append(out, fill(factRequestsFail, "{server}", strings.ToLower(server[:1])+server[1:]))
	case p.OtherAppsServed:
		out = append(out, fill(factNotAnsweringUs, "{server}", server))
	default:
		out = append(out, fill(factNotAnswering, "{server}", server))
	}
	if adminSentence != "" {
		out = append(out, adminSentence)
	}
	if p.SharedApp.Family != "" && p.Rule != problems.RuleSameAppVersion {
		app := attemptlog.AppName(p.SharedApp.Family) + " " + p.SharedApp.Version
		if p.People == 1 {
			out = append(out, fill(factUses, "{app}", app))
		} else {
			out = append(out, fill(factAllUse, "{app}", app))
		}
	}
	if p.OtherAppsServed {
		out = append(out, factOtherApps)
	}
	return strings.Join(out, " ")
}

func hostOf(key string) string {
	if u, err := url.Parse(key); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return key
}

func allSavedCredential(p problems.Problem) bool {
	for _, e := range p.Episodes {
		if !slices.Contains(savedCredentialReasons, e.Reason) {
			return false
		}
	}
	return true
}

func sharedReason(p problems.Problem) string {
	reason := p.Episodes[0].Reason
	for _, e := range p.Episodes[1:] {
		if e.Reason != reason {
			return ""
		}
	}
	return reason
}

// problemServers counts the distinct servers and gateway members the problem's people fail on.
func problemServers(p problems.Problem) int {
	seen := map[string]bool{}
	for _, e := range p.Episodes {
		seen[e.ServerKind+"/"+e.ServerID+"/"+e.MemberID] = true
	}
	return len(seen)
}

// serversPhrase names the servers the problem's people fail on: "Linear", "Linear and GitHub", "Linear and 2 other servers".
func serversPhrase(n names, p problems.Problem) string {
	var named []string
	seen := map[string]bool{}
	for _, e := range p.Episodes {
		if !e.ReachedServer() {
			continue
		}
		key := e.ServerID + "/" + e.MemberID
		if seen[key] {
			continue
		}
		seen[key] = true
		name := episodeServerName(n, e)
		if name == "" {
			name = "an MCP server"
		}
		named = append(named, name)
	}
	switch len(named) {
	case 0:
		return ""
	case 1:
		return named[0]
	case 2:
		return named[0] + " and " + named[1]
	default:
		return fmt.Sprintf("%s and %d other servers", named[0], len(named)-1)
	}
}
