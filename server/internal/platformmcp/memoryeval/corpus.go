// Package memoryeval holds the synthetic corpus, question set and mechanical
// checks used to evaluate team chat memory in the Platform MCP.
//
// The corpus is a fictional freight software team: four users, about 150
// chats over 60 days in a main project, three private sessions, and a second
// project. It plants decisions, reversals and secrets so that answers can be
// graded and leaks or isolation failures can be detected by exact match. It
// is plain data with deterministic chat IDs, so every run sees the same
// corpus and citations can be traced back to it.
package memoryeval

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProjectKey names one of the corpus projects. Loaders map it to a real
// project.
type ProjectKey string

const (
	// ProjectMain is the project whose memory is under evaluation.
	ProjectMain ProjectKey = "main"

	// ProjectOther is a second project in the same organization. None of its
	// content may appear in answers about ProjectMain.
	ProjectOther ProjectKey = "other"
)

const (
	// RoleUser marks a message written by a person.
	RoleUser = "user"

	// RoleAssistant marks a message written by the agent.
	RoleAssistant = "assistant"
)

const (
	// corpusDays is how far back the corpus history reaches.
	corpusDays = 60

	// messageGap separates consecutive messages in a chat.
	messageGap = 3 * time.Minute
)

// chatNamespace derives deterministic chat IDs from chat keys, so a citation
// in a run can always be mapped back to its corpus chat.
var chatNamespace = uuid.MustParse("6f0c2a3e-5b7d-4c1a-9e8f-2d4b6a8c0e1f")

// User is a synthetic team member.
type User struct {
	// Handle is a short stable identifier that loaders turn into user IDs.
	Handle string

	// Name is the display name.
	Name string
}

// Message is one chat message.
type Message struct {
	// Role is RoleUser or RoleAssistant.
	Role string

	// Text is the message content, with secrets already substituted.
	Text string

	// At is when the message was sent.
	At time.Time
}

// Chat is one synthetic chat session.
type Chat struct {
	// Key is the stable corpus key questions use to name their sources.
	Key string

	// ID is derived from Key and is what loaders store as the chat ID.
	ID uuid.UUID

	// Project is the project the chat belongs to.
	Project ProjectKey

	// Owner is the handle of the user who ran the session.
	Owner string

	// Title is the chat title.
	Title string

	// Private marks a session that must never reach team memory.
	Private bool

	// Canaries are phrases unique to this chat. They are set only on chats
	// that team memory for ProjectMain must never surface.
	Canaries []string

	// Messages alternate between user and assistant, starting with the user.
	Messages []Message
}

// StartedAt is when the first message was sent.
func (c Chat) StartedAt() time.Time {
	return c.Messages[0].At
}

// Text joins the title and every message, for evidence and canary matching.
func (c Chat) Text() string {
	var b strings.Builder
	b.WriteString(c.Title)
	for _, m := range c.Messages {
		b.WriteString("\n")
		b.WriteString(m.Text)
	}
	return b.String()
}

// Isolated reports whether team memory for ProjectMain must never surface
// this chat.
func (c Chat) Isolated() bool {
	return c.Private || c.Project != ProjectMain
}

// Corpus is the full synthetic dataset.
type Corpus struct {
	// Now anchors every timestamp; history runs from Now back 60 days.
	Now time.Time

	// Users are the four team members.
	Users []User

	// Chats are all chats in both projects, oldest first.
	Chats []Chat

	// Secrets are the planted sensitive values, one per masking rule.
	Secrets []Secret

	// Questions are the 40 graded questions.
	Questions []Question
}

// chatSpec is the authored form of a chat, before timestamps and secrets are
// resolved.
type chatSpec struct {
	key      string
	project  ProjectKey
	owner    string
	daysAgo  int
	hour     int
	title    string
	private  bool
	canaries []string
	turns    []string
}

// team builds a team chat in the main project. Turns alternate between the
// user and the assistant, starting with the user.
func team(key, owner string, daysAgo int, title string, turns ...string) chatSpec {
	return chatSpec{key: key, project: ProjectMain, owner: owner, daysAgo: daysAgo, hour: 0, title: title, private: false, canaries: nil, turns: turns}
}

// private builds a private session in the main project.
func private(key, owner string, daysAgo int, title string, canaries []string, turns ...string) chatSpec {
	return chatSpec{key: key, project: ProjectMain, owner: owner, daysAgo: daysAgo, hour: 0, title: title, private: true, canaries: canaries, turns: turns}
}

// other builds a team chat in the other project.
func other(key, owner string, daysAgo int, title string, canaries []string, turns ...string) chatSpec {
	return chatSpec{key: key, project: ProjectOther, owner: owner, daysAgo: daysAgo, hour: 0, title: title, private: false, canaries: canaries, turns: turns}
}

var users = []User{
	{Handle: "amara", Name: "Amara Okafor"},
	{Handle: "jonas", Name: "Jonas Lindqvist"},
	{Handle: "priya", Name: "Priya Raman"},
	{Handle: "mateo", Name: "Mateo Silva"},
}

// Build returns the corpus anchored at now. The same now always yields the
// same corpus.
func Build(now time.Time) *Corpus {
	now = now.UTC().Truncate(time.Minute)
	secrets := plantedSecrets()

	specs := slices.Concat(decisionChats(), timelineChats(), secretChats(), isolatedChats(), fillerChats())
	chats := make([]Chat, 0, len(specs))
	for i, spec := range specs {
		chats = append(chats, spec.build(now, i, secrets))
	}
	slices.SortStableFunc(chats, func(a, b Chat) int { return a.StartedAt().Compare(b.StartedAt()) })

	return &Corpus{Now: now, Users: slices.Clone(users), Chats: chats, Secrets: secrets, Questions: questions()}
}

// build resolves timestamps and secret placeholders. ordinal spreads chats
// that share a day across working hours when no hour is set.
func (s chatSpec) build(now time.Time, ordinal int, secrets []Secret) Chat {
	const (
		// firstHour and workingHours place chats between 09:00 and 17:59 UTC.
		firstHour    = 9
		workingHours = 9
	)
	hour := s.hour
	if hour == 0 {
		hour = firstHour + int(fnv32(s.key)%workingHours)
	}
	day := now.Truncate(24*time.Hour).AddDate(0, 0, -s.daysAgo)
	start := day.Add(time.Duration(hour)*time.Hour + time.Duration(ordinal%60)*time.Minute)

	messages := make([]Message, len(s.turns))
	for i, turn := range s.turns {
		role := RoleUser
		if i%2 == 1 {
			role = RoleAssistant
		}
		messages[i] = Message{Role: role, Text: substituteSecrets(turn, secrets), At: start.Add(time.Duration(i) * messageGap)}
	}
	return Chat{
		Key:      s.key,
		ID:       ChatID(s.key),
		Project:  s.project,
		Owner:    s.owner,
		Title:    s.title,
		Private:  s.private,
		Canaries: slices.Clone(s.canaries),
		Messages: messages,
	}
}

// ChatID is the deterministic chat ID for a corpus key.
func ChatID(key string) uuid.UUID {
	return uuid.NewSHA1(chatNamespace, []byte(key))
}

// Chat returns the chat with the given key.
func (c *Corpus) Chat(key string) (Chat, bool) {
	i := slices.IndexFunc(c.Chats, func(chat Chat) bool { return chat.Key == key })
	if i < 0 {
		var missing Chat
		return missing, false
	}
	return c.Chats[i], true
}

// ChatByID returns the chat with the given ID.
func (c *Corpus) ChatByID(id uuid.UUID) (Chat, bool) {
	i := slices.IndexFunc(c.Chats, func(chat Chat) bool { return chat.ID == id })
	if i < 0 {
		var missing Chat
		return missing, false
	}
	return c.Chats[i], true
}

// fnv32 is a small stable hash for spreading chats across hours.
func fnv32(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := range len(s) {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}
