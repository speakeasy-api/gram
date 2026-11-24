package mcpconnectionhealth

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
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
	problems.RuleOnePerson:      {"{scope} can't sign in{place}", "{scope} can't connect{place}"},
}

// connectHeadline names the servers whatever the rule.
const connectHeadline = "{people} {need} to connect their account for {servers}"

// The fact sentences. Each states only what the episodes establish.
const (
	factOn            = "On {servers}."
	factOnAndNoServer = "On {servers}, and before reaching an MCP server."
	factAppSignIn     = "The app's sign-in to Speakeasy fails."
	factServiceSignIn = "Signing in to {service} fails."
	factSavedSignIn   = "Requests with the saved sign-in for {service} are failing."
	factRequestsFail  = "Requests to {server} are failing."
	factAllUse        = "All use {app}."
	factUses          = "Uses {app}."
	factOtherApp      = "Another app worked on {server} in this time."
	factOtherAppSome  = "Another app worked on at least one of {servers} in this time."
	factOtherAppEach  = "Another app worked on each of {servers} in this time."

	factConnect        = "Their apps asked for {servers}. Each person has to connect their own account for it, or reconnect it if it expired."
	factConnectEach    = "Their apps asked for {servers}. Each person has to connect their own account for the server their app asked for, or reconnect it if it expired."
	factConnectOne     = "{whose} asked for {servers}. They have to connect their own account for it, or reconnect it if it expired."
	factConnectOneEach = "{whose} asked for {servers}. They have to connect their own account for each of them, or reconnect any that expired."
	factConnectWhere   = "They can do that from the link their app shows."
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

// headlinePlace names where one person fails: " to Linear", " to 3 MCP servers", or nothing when no server was reached.
const (
	headlineToServer  = " to {server}"
	headlineToServers = " to {count} MCP servers"
)

// needsOwnConnection reports that every run ended on the token step with no usable connection for its server.
func needsOwnConnection(p problems.Problem) bool {
	if p.Part != mcpconnectionattempts.PartAppSignIn || len(p.Episodes) == 0 {
		return false
	}
	for _, e := range p.Episodes {
		if e.Step != mcpconnectionattempts.StepInboundToken.String() || e.Reason != mcpconnectionattempts.CodeInvalidRemoteSession.String() || !e.OnMCPServer() {
			return false
		}
	}
	return true
}

func headline(n names, p problems.Problem, scope string) string {
	part := 1
	if p.Part == mcpconnectionattempts.PartAppSignIn {
		part = 0
	}
	if needsOwnConnection(p) {
		people, need := fmt.Sprintf("%d people", p.People), "need"
		if p.Rule == problems.RuleOnePerson {
			people, need = scope, "needs"
		}
		return fill(connectHeadline, "{people}", people, "{need}", need, "{servers}", connectServers(n, p))
	}
	place := ""
	if p.Rule == problems.RuleOnePerson {
		switch places := namedPlaces(p); {
		case reachedServers(p) > 1:
			place = fill(headlineToServers, "{count}", strconv.Itoa(reachedServers(p)))
		case len(places) == 1:
			name := episodeServerName(n, places[0])
			if name == "" {
				name = "an MCP server"
			}
			place = fill(headlineToServer, "{server}", name)
		}
	}
	return fill(headlines[p.Rule][part], "{people}", fmt.Sprintf("%d people", p.People), "{scope}", scope, "{place}", place)
}

// fact states what failed in plain words, using the catalog's own sentences.
func fact(n names, p problems.Problem) string {
	if needsOwnConnection(p) {
		return strings.Join(append([]string{connectFact(n, p), factConnectWhere}, sharedAppFact(p, false)...), " ")
	}
	var out []string
	servers := problemServers(p)
	// Whether the headline or the fact has named the server or servers.
	named := p.Rule == problems.RuleSameServer
	if p.Rule == problems.RuleOnePerson || p.Rule == problems.RuleSameAppVersion {
		if where := serversPhrase(n, p); where != "" {
			on := factOn
			if slices.ContainsFunc(p.Episodes, func(e mcpconnectionattempts.FailingPerson) bool { return !e.OnMCPServer() }) {
				on = factOnAndNoServer
			}
			out = append(out, fill(on, "{servers}", where))
			named = true
		}
	}
	service, server, several := "the service", "the server", "these servers"
	switch {
	case p.Rule == problems.RuleSameService:
		service = hostOf(p.Scope.Key)
		server, several = "servers using "+service, "the servers using "+service
	case servers > 1 && named:
		server = "these servers"
	case servers > 1:
		server, several = "the servers", "the servers"
	case named:
		server = "this server"
	}
	// The catalog's administrator sentence is about one attempt on one server. Its
	// person-facing sentence speaks to the person, so the fact line never uses it.
	reason := sharedReason(p)
	meaning, _ := mcpconnectionattempts.MeaningOf(reason)
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
		default:
			out = append(out, fill(factSavedSignIn, "{service}", service))
		}
	case p.Part == mcpconnectionattempts.PartServiceSignIn:
		out = append(out, fill(factServiceSignIn, "{service}", service))
	// What a server returned is not proof it is down: say what was recorded.
	case adminSentence != "":
		out = append(out, adminSentence)
		adminSentence = ""
	default:
		out = append(out, fill(factRequestsFail, "{server}", server))
	}
	if adminSentence != "" {
		out = append(out, adminSentence)
	}
	out = append(out, sharedAppFact(p, p.Rule == problems.RuleSameAppVersion)...)
	if p.OtherAppsWorked {
		switch {
		// An app version's servers each have another app working; its people on no server are not about a server.
		case p.Rule == problems.RuleSameAppVersion && reachedServers(p) == 1:
			out = append(out, fill(factOtherApp, "{server}", "this server"))
		case p.Rule == problems.RuleSameAppVersion:
			out = append(out, fill(factOtherAppEach, "{servers}", "these servers"))
		case servers == 1:
			out = append(out, fill(factOtherApp, "{server}", server))
		default:
			out = append(out, fill(factOtherAppSome, "{servers}", several))
		}
	}
	return strings.Join(out, " ")
}

// connectFact says who has to connect their own account for which servers.
func connectFact(n names, p problems.Problem) string {
	template := factConnect
	switch one, each := p.People == 1, reachedServers(p) > 1; {
	case one && each:
		template = factConnectOneEach
	case one:
		template = factConnectOne
	case each:
		template = factConnectEach
	}
	whose := "Their app"
	if appsOf(p) > 1 {
		whose = "Their apps"
	}
	return fill(template, "{servers}", serversPhrase(n, p), "{whose}", whose)
}

// connectServers names the servers in a headline: "Linear", "Linear and GitHub", "3 MCP servers".
func connectServers(n names, p problems.Problem) string {
	named := make([]string, 0, 2)
	for _, e := range namedPlaces(p) {
		name := episodeServerName(n, e)
		if name == "" {
			name = "an MCP server"
		}
		named = append(named, name)
	}
	switch reached := reachedServers(p); {
	case reached == 1 && len(named) == 1:
		return named[0]
	case reached == 2 && len(named) == 2:
		return named[0] + " and " + named[1]
	default:
		return fmt.Sprintf("%d MCP servers", reached)
	}
}

// appsOf counts the distinct apps of the problem's runs, at least each run's own total.
func appsOf(p problems.Problem) int {
	seen := map[string]bool{}
	most := 0
	for _, e := range p.Episodes {
		for _, a := range e.Apps {
			seen[a.Key+"|"+a.Family+"|"+a.Version] = true
		}
		most = max(most, e.AppsTotal)
	}
	return max(len(seen), most)
}

// sharedAppFact names the app every person uses, unless the headline does.
func sharedAppFact(p problems.Problem, headlineNamesApp bool) []string {
	if p.SharedApp.Family == "" || headlineNamesApp {
		return nil
	}
	app := attemptlog.AppName(p.SharedApp.Family) + " " + p.SharedApp.Version
	if p.People == 1 {
		return []string{fill(factUses, "{app}", app)}
	}
	return []string{fill(factAllUse, "{app}", app)}
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

// problemServers counts the distinct servers the problem's people fail on.
func problemServers(p problems.Problem) int {
	seen := map[string]bool{}
	for _, e := range p.Episodes {
		seen[problems.ServerOf(e)] = true
	}
	return len(seen)
}

// namedPlaces are the first two distinct servers or gateway members the problem's people fail on, the ones its fact names.
func namedPlaces(p problems.Problem) []mcpconnectionattempts.FailingPerson {
	var out []mcpconnectionattempts.FailingPerson
	for _, e := range p.Episodes {
		if !e.OnMCPServer() || slices.ContainsFunc(out, func(o mcpconnectionattempts.FailingPerson) bool {
			return problems.ServerOf(o) == problems.ServerOf(e)
		}) {
			continue
		}
		if out = append(out, e); len(out) == 2 {
			break
		}
	}
	return out
}

// serversPhrase names the servers the problem's people fail on: "Linear", "Linear and GitHub", "Linear and 2 other servers".
func serversPhrase(n names, p problems.Problem) string {
	named := make([]string, 0, 2)
	for _, e := range namedPlaces(p) {
		name := episodeServerName(n, e)
		if name == "" {
			name = "an MCP server"
		}
		named = append(named, name)
	}
	reached := reachedServers(p)
	switch {
	case len(named) == 0:
		return ""
	case reached == 1:
		return named[0]
	case reached == 2:
		return named[0] + " and " + named[1]
	default:
		return fmt.Sprintf("%s and %d other servers", named[0], reached-1)
	}
}

// reachedServers counts the distinct servers the problem's people fail on, a gateway member and the same server reached directly being one.
func reachedServers(p problems.Problem) int {
	seen := map[string]bool{}
	for _, e := range p.Episodes {
		if e.OnMCPServer() {
			seen[problems.ServerOf(e)] = true
		}
	}
	return len(seen)
}
