// Package supportmatrix holds the support matrix: which integration methods
// apply to which platforms, and what each one delivers per capability. It is
// code, not data: matrix.yaml is embedded in the binary, edited by pull
// request, and validated by the tests in this package. Nothing writes it at
// runtime.
package supportmatrix

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

//go:embed matrix.yaml
var raw []byte

// MaxNoteLength bounds every free-text field.
const MaxNoteLength = 10000

// Status is what a cell says about a capability.
type Status string

const (
	StatusSupported     Status = "supported"
	StatusPartial       Status = "partial"
	StatusUnimplemented Status = "unimplemented"
	StatusImpossible    Status = "impossible"
	StatusNA            Status = "na"
	StatusUnknown       Status = "unknown"
)

// Applicability says whether a method applies to a platform at all.
type Applicability string

const (
	Applicable        Applicability = "applicable"
	NotApplicable     Applicability = "na"
	UnknownApplicable Applicability = "unknown"
)

// Eligibility says whether an account type can use a method on a platform.
type Eligibility string

const (
	Eligible           Eligibility = "supported"
	Ineligible         Eligibility = "unsupported"
	UnknownEligibility Eligibility = "unknown"
)

// Account is a customer account type. The matrix answers per account type
// because vendors sell most integrations to organizations only.
type Account string

const (
	AccountPersonal   Account = "personal"
	AccountTeam       Account = "team"
	AccountEnterprise Account = "enterprise"
)

// Accounts lists every account type in display order.
var Accounts = []Account{AccountPersonal, AccountTeam, AccountEnterprise}

// OSSupport is what is known about a method on one operating system.
type OSSupport string

const (
	OSSupported OSSupport = "supported"
	// OSVerify: believed supported, not yet verified.
	OSVerify OSSupport = "verify"
)

// Fact is one claim: a status with the note behind it and whether someone
// still has to verify it.
type Fact struct {
	Status Status `yaml:"status"`
	Note   string `yaml:"note"`
	Verify bool   `yaml:"verify"`
}

// Capability is something an integration can deliver, such as session
// tracking or blocking.
type Capability struct {
	ID    string `yaml:"id"`
	Name  string `yaml:"name"`
	Group string `yaml:"group"`
}

// Platform is an upstream product surface, such as the Claude Code CLI.
type Platform struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	Vendor  string `yaml:"vendor"`
	Family  string `yaml:"family"`
	Surface string `yaml:"surface"`
}

// AccountEligibility says which account types can use a method on a platform.
type AccountEligibility struct {
	Personal   Eligibility `yaml:"personal"`
	Team       Eligibility `yaml:"team"`
	Enterprise Eligibility `yaml:"enterprise"`
}

// For returns the eligibility of one account type.
func (a AccountEligibility) For(account Account) Eligibility {
	switch account {
	case AccountPersonal:
		return a.Personal
	case AccountTeam:
		return a.Team
	case AccountEnterprise:
		return a.Enterprise
	default:
		return UnknownEligibility
	}
}

// OS records what is known per operating system. An absent value means
// nothing is known.
type OS struct {
	Mac     OSSupport `yaml:"mac,omitempty"`
	Windows OSSupport `yaml:"windows,omitempty"`
	Linux   OSSupport `yaml:"linux,omitempty"`
}

// PlatformSupport is one method on one platform: whether it applies, who can
// use it, and one explicit cell per capability when it applies.
type PlatformSupport struct {
	Platform      string             `yaml:"platform"`
	Applicability Applicability      `yaml:"applicability"`
	Accounts      AccountEligibility `yaml:"accounts"`
	OS            *OS                `yaml:"os,omitempty"`
	// Note is for readers: a qualifier that explains the cells.
	Note string `yaml:"note,omitempty"`
	// Cells hold one fact per capability. Present only when the method
	// applies; every cell of a method that does not apply is not applicable,
	// and every cell of one whose applicability is unknown is unknown.
	Cells map[string]Fact `yaml:"cells,omitempty"`
}

// Method is an integration method, such as device management or hooks.
type Method struct {
	ID     string `yaml:"id"`
	Name   string `yaml:"name"`
	Vendor string `yaml:"vendor"`
	// Plans is prose about plan eligibility, for readers only.
	Plans string `yaml:"plans"`
	// Claims are what the method delivers per capability, platform aside.
	Claims map[string]Fact `yaml:"claims"`
	// Platforms lists every platform once, in the matrix's platform order.
	Platforms []PlatformSupport `yaml:"platforms"`

	byPlatform map[string]*PlatformSupport
}

// Support returns the method's entry for a platform.
func (m *Method) Support(platformID string) (*PlatformSupport, bool) {
	support, ok := m.byPlatform[platformID]
	return support, ok
}

// Plan is an organizational plan a vendor sells, the kind an organization's
// recorded stack names. Its vendor must sell a platform.
type Plan struct {
	ID     string `yaml:"id"`
	Vendor string `yaml:"vendor"`
	Name   string `yaml:"name"`
}

// Matrix is the whole support matrix.
type Matrix struct {
	Capabilities []Capability `yaml:"capabilities"`
	Platforms    []Platform   `yaml:"platforms"`
	Plans        []Plan       `yaml:"plans,omitempty"`
	Methods      []Method     `yaml:"methods"`

	// Revision is the hex SHA-256 of the file, so a reader can tell one
	// deployed matrix from another.
	Revision string `yaml:"-"`

	capabilities map[string]*Capability
	platforms    map[string]*Platform
	plans        map[string]*Plan
	methods      map[string]*Method
}

// Capability looks a capability up by id.
func (m *Matrix) Capability(id string) (*Capability, bool) {
	c, ok := m.capabilities[id]
	return c, ok
}

// Platform looks a platform up by id.
func (m *Matrix) Platform(id string) (*Platform, bool) {
	p, ok := m.platforms[id]
	return p, ok
}

// Plan looks a plan up by id.
func (m *Matrix) Plan(id string) (*Plan, bool) {
	plan, ok := m.plans[id]
	return plan, ok
}

// Method looks a method up by id.
func (m *Matrix) Method(id string) (*Method, bool) {
	method, ok := m.methods[id]
	return method, ok
}

// Cell is the fact for one capability of a method on a platform, across every
// account type: a method that does not apply yields not applicable and one
// whose applicability is unknown yields unknown.
func (s *PlatformSupport) Cell(capabilityID string) Fact {
	switch s.Applicability {
	case NotApplicable:
		return Fact{Status: StatusNA, Note: "Method does not apply to this platform", Verify: false}
	case Applicable:
		if fact, ok := s.Cells[capabilityID]; ok {
			return fact
		}
		return Fact{Status: StatusUnknown, Note: "", Verify: false}
	default:
		return Fact{Status: StatusUnknown, Note: "", Verify: false}
	}
}

// CellFor narrows a cell to one account type: an ineligible account cannot
// have the capability, and unknown eligibility leaves it unknown and to be
// verified. Not applicable stays not applicable.
func (s *PlatformSupport) CellFor(capabilityID string, account Account) Fact {
	fact := s.Cell(capabilityID)
	if fact.Status == StatusNA {
		return fact
	}
	switch s.Accounts.For(account) {
	case Ineligible:
		return Fact{Status: StatusImpossible, Note: fmt.Sprintf("%s accounts are not eligible for this method", strings.ToUpper(string(account[:1]))+string(account[1:])), Verify: false}
	case Eligible:
		return fact
	default:
		return Fact{Status: StatusUnknown, Note: "Account eligibility is not known", Verify: true}
	}
}

// Load parses and validates the embedded matrix.
func Load() (*Matrix, error) {
	return Parse(raw)
}

var (
	once    sync.Once
	current *Matrix
	errLoad error
)

// Current returns the embedded matrix, parsed once. The first caller at
// start-up fails when the file is malformed, so a bad edit never serves.
func Current() (*Matrix, error) {
	once.Do(func() { current, errLoad = Load() })
	return current, errLoad
}

// Parse decodes a matrix strictly and validates it.
func Parse(data []byte) (*Matrix, error) {
	var matrix Matrix
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&matrix); err != nil {
		return nil, fmt.Errorf("decode support matrix: %w", err)
	}
	if err := matrix.validate(); err != nil {
		return nil, fmt.Errorf("invalid support matrix: %w", err)
	}
	sum := sha256.Sum256(data)
	matrix.Revision = hex.EncodeToString(sum[:])
	return &matrix, nil
}

var (
	slugPattern  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	uuidPattern  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

func (m *Matrix) validate() error {
	m.capabilities = make(map[string]*Capability, len(m.Capabilities))
	m.platforms = make(map[string]*Platform, len(m.Platforms))
	m.methods = make(map[string]*Method, len(m.Methods))
	if len(m.Capabilities) == 0 || len(m.Platforms) == 0 || len(m.Methods) == 0 {
		return fmt.Errorf("capabilities, platforms and methods must each have an entry")
	}
	for i := range m.Capabilities {
		c := &m.Capabilities[i]
		if err := checkSlug("capability", c.ID); err != nil {
			return err
		}
		if _, dup := m.capabilities[c.ID]; dup {
			return fmt.Errorf("capability %q is listed twice", c.ID)
		}
		if err := checkText("capability "+c.ID, "name", c.Name, true); err != nil {
			return err
		}
		if err := checkText("capability "+c.ID, "group", c.Group, true); err != nil {
			return err
		}
		m.capabilities[c.ID] = c
	}
	for i := range m.Platforms {
		p := &m.Platforms[i]
		if err := checkSlug("platform", p.ID); err != nil {
			return err
		}
		if _, dup := m.platforms[p.ID]; dup {
			return fmt.Errorf("platform %q is listed twice", p.ID)
		}
		for name, value := range map[string]string{"name": p.Name, "vendor": p.Vendor, "family": p.Family, "surface": p.Surface} {
			if err := checkText("platform "+p.ID, name, value, true); err != nil {
				return err
			}
		}
		m.platforms[p.ID] = p
	}
	vendors := make(map[string]bool, len(m.Platforms))
	for i := range m.Platforms {
		vendors[m.Platforms[i].Vendor] = true
	}
	m.plans = make(map[string]*Plan, len(m.Plans))
	for i := range m.Plans {
		plan := &m.Plans[i]
		if err := checkSlug("plan", plan.ID); err != nil {
			return err
		}
		if _, dup := m.plans[plan.ID]; dup {
			return fmt.Errorf("plan %q is listed twice", plan.ID)
		}
		for name, value := range map[string]string{"name": plan.Name, "vendor": plan.Vendor} {
			if err := checkText("plan "+plan.ID, name, value, true); err != nil {
				return err
			}
		}
		if !vendors[plan.Vendor] {
			return fmt.Errorf("plan %q names vendor %q, which sells no platform", plan.ID, plan.Vendor)
		}
		m.plans[plan.ID] = plan
	}
	for i := range m.Methods {
		method := &m.Methods[i]
		if err := checkSlug("method", method.ID); err != nil {
			return err
		}
		if _, dup := m.methods[method.ID]; dup {
			return fmt.Errorf("method %q is listed twice", method.ID)
		}
		if err := checkText("method "+method.ID, "name", method.Name, true); err != nil {
			return err
		}
		if err := checkText("method "+method.ID, "vendor", method.Vendor, true); err != nil {
			return err
		}
		if err := checkText("method "+method.ID, "plans", method.Plans, false); err != nil {
			return err
		}
		if err := m.checkCells("method "+method.ID+" claims", method.Claims); err != nil {
			return err
		}
		method.byPlatform = make(map[string]*PlatformSupport, len(method.Platforms))
		for j := range method.Platforms {
			support := &method.Platforms[j]
			where := fmt.Sprintf("method %s on %s", method.ID, support.Platform)
			if _, ok := m.platforms[support.Platform]; !ok {
				return fmt.Errorf("%s: unknown platform", where)
			}
			if _, dup := method.byPlatform[support.Platform]; dup {
				return fmt.Errorf("%s: listed twice", where)
			}
			switch support.Applicability {
			case Applicable, NotApplicable, UnknownApplicable:
			default:
				return fmt.Errorf("%s: applicability %q is not applicable, na or unknown", where, support.Applicability)
			}
			for _, account := range Accounts {
				switch support.Accounts.For(account) {
				case Eligible, Ineligible, UnknownEligibility:
				default:
					return fmt.Errorf("%s: %s accounts %q is not supported, unsupported or unknown", where, account, support.Accounts.For(account))
				}
			}
			if support.OS != nil {
				for name, value := range map[string]OSSupport{"mac": support.OS.Mac, "windows": support.OS.Windows, "linux": support.OS.Linux} {
					switch value {
					case "", OSSupported, OSVerify:
					default:
						return fmt.Errorf("%s: os %s %q is not supported or verify", where, name, value)
					}
				}
			}
			if err := checkText(where, "note", support.Note, false); err != nil {
				return err
			}
			if support.Applicability == Applicable {
				if err := m.checkCells(where+" cells", support.Cells); err != nil {
					return err
				}
			} else if len(support.Cells) > 0 {
				return fmt.Errorf("%s: cells are listed although the method does not apply", where)
			}
			method.byPlatform[support.Platform] = support
		}
		if len(method.Platforms) != len(m.Platforms) {
			return fmt.Errorf("method %s lists %d platforms, the matrix has %d", method.ID, len(method.Platforms), len(m.Platforms))
		}
		m.methods[method.ID] = method
	}
	return nil
}

// checkCells wants exactly one valid fact per capability.
func (m *Matrix) checkCells(where string, cells map[string]Fact) error {
	for id, fact := range cells {
		if _, ok := m.capabilities[id]; !ok {
			return fmt.Errorf("%s: unknown capability %q", where, id)
		}
		switch fact.Status {
		case StatusSupported, StatusPartial, StatusUnimplemented, StatusImpossible, StatusNA, StatusUnknown:
		default:
			return fmt.Errorf("%s: %s status %q is not a status", where, id, fact.Status)
		}
		if err := checkText(where+" "+id, "note", fact.Note, false); err != nil {
			return err
		}
		if fact.Status == StatusPartial && strings.TrimSpace(fact.Note) == "" {
			return fmt.Errorf("%s: %s is partial, which needs a note saying what is missing", where, id)
		}
	}
	if len(cells) != len(m.Capabilities) {
		return fmt.Errorf("%s: %d capabilities listed, the matrix has %d", where, len(cells), len(m.Capabilities))
	}
	return nil
}

func checkSlug(kind, id string) error {
	if !slugPattern.MatchString(id) {
		return fmt.Errorf("%s id %q must be lower-case words joined by dashes", kind, id)
	}
	return nil
}

// checkText keeps free text bounded and free of identifiers that do not
// belong in a public repository.
func checkText(where, field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s: %s is required", where, field)
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s: %s must be valid text without NUL characters", where, field)
	}
	if utf8.RuneCountInString(value) > MaxNoteLength {
		return fmt.Errorf("%s: %s must be at most %d characters", where, field, MaxNoteLength)
	}
	if emailPattern.MatchString(value) || uuidPattern.MatchString(value) {
		return fmt.Errorf("%s: %s must not contain an email address or an id", where, field)
	}
	return nil
}
