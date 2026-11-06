package zedspike

import (
	"errors"
	"fmt"
	"strings"
)

// GrantRow is one principal_grants row, unchanged from today.
type GrantRow struct {
	PrincipalURN string            // user:<id>, user:all, role:<kind>:<id>, agent:<id>, api_key:<id>
	Scope        Scope             // mcp:connect
	Selector     map[string]string // {"resource_kind":"mcp","resource_id":"*","project_id":"<P>","tool":"search"}
	Inferred     bool              // written by the auth permission backfill
}

// Relationship is a grant row read as a SpiceDB relationship. Nothing of this
// shape is stored: the loader builds it from rows in memory.
type Relationship struct {
	ResourceType    string
	ResourceID      string
	Relation        string
	SubjectType     string
	SubjectID       string // "*" for user:*
	SubjectRelation string // "member" for role#member
	Caveat          string
	CaveatContext   map[string]any
}

// String renders the relationship in SpiceDB's tuple syntax.
func (r Relationship) String() string {
	subject := r.SubjectType + ":" + r.SubjectID
	if r.SubjectRelation != "" {
		subject += "#" + r.SubjectRelation
	}
	out := fmt.Sprintf("%s:%s#%s@%s", r.ResourceType, r.ResourceID, r.Relation, subject)
	if r.Caveat != "" {
		out += "[" + r.Caveat + "]"
	}
	return out
}

// ErrUnmigratedRow is a row that names a project ID as its resource_id. Those
// rows are rewritten once to resource_id "*" with project_id before the
// schema becomes authoritative; see the README.
var ErrUnmigratedRow = errors.New("row names a project as its resource")

// scopeResourceType is the definition a scope's rows describe.
var scopeResourceType = map[string]string{
	"root":        "organization",
	"org":         "organization",
	"project":     "project",
	"plugin":      "project",
	"log":         "project",
	"mcp":         "mcp_server",
	"environment": "environment",
	"skill":       "skill",
	"assistant":   "assistant",
	"inference":   "inference",
	"chat":        "chat",
	"risk_policy": "risk_policy",
}

func resourceTypeForScope(scope Scope) string {
	if scope == ScopeMCPAuth || scope == ScopeMCPBlockedAuth {
		return "mcp_auth_client"
	}
	prefix, _, _ := strings.Cut(string(scope), ":")
	return scopeResourceType[prefix]
}

// Land reads one grant row as the relationships it stands for:
//
//   - the subject comes from principal_urn;
//   - the object comes from the selector: a concrete resource_id names that
//     resource, "*" with project_id names the project, "*" alone the
//     organization;
//   - the relation is the scope with ":" as "_", plus _direct for a user's
//     own concrete grant and _own for a user's own block;
//   - the remaining selector keys fill the caveat's granted_ parameters.
func (r *Registry) Land(organizationID string, row GrantRow) ([]Relationship, error) {
	info, ok := ScopeCatalogue[row.Scope]
	if !ok {
		return nil, fmt.Errorf("scope %q is not in the schema", row.Scope)
	}
	subjectType, subjectID, subjectRelation, err := subjectFor(row.PrincipalURN)
	if err != nil {
		return nil, err
	}

	resourceType := resourceTypeForScope(row.Scope)
	objectType, objectID := "organization", organizationID
	resourceID := row.Selector["resource_id"]
	projectID := row.Selector[SelectorProjectID]
	concrete := resourceID != "" && resourceID != "*"
	switch {
	case concrete && projectID != "" && resourceID == projectID && resourceType != "project":
		return nil, fmt.Errorf("%w: %s %v", ErrUnmigratedRow, row.Scope, row.Selector)
	case concrete && resourceType != "organization":
		objectType, objectID = resourceType, resourceID
	case projectID != "" && landsOnProject(resourceType):
		objectType, objectID = "project", projectID
	}

	namedUser := subjectType == "user" && subjectID != "*"
	relation := strings.Replace(string(row.Scope), ":", "_", 1)
	var relations []string
	switch {
	case info.Block && namedUser:
		relations = []string{relation + "_own"}
	case info.Block:
		relations = []string{relation}
	case namedUser && concrete && !row.Inferred && r.hasRelation(objectType, relation+"_direct"):
		relations = []string{relation, relation + "_direct"}
	default:
		relations = []string{relation}
	}

	out := make([]Relationship, 0, len(relations))
	for _, name := range relations {
		def := r.Types[objectType].Relations[name]
		if def == nil {
			return nil, fmt.Errorf("%s has no relation %s for row %s %v", objectType, name, row.Scope, row.Selector)
		}
		rel := Relationship{
			ResourceType: objectType, ResourceID: objectID, Relation: name,
			SubjectType: subjectType, SubjectID: subjectID, SubjectRelation: subjectRelation,
		}
		if caveat := def.caveatFor(subjectType, subjectID == "*", subjectRelation); caveat != "" {
			rel.Caveat = caveat
			rel.CaveatContext = grantedParams(r.Caveats[caveat], row.Selector)
		}
		out = append(out, rel)
	}
	return out, nil
}

// Membership reads one role assignment as role:<id>#member@user:<id>.
func Membership(roleURN, userID string) (Relationship, error) {
	_, roleID, _, err := subjectFor(roleURN)
	if err != nil {
		return Relationship{}, err
	}
	return Relationship{ResourceType: "role", ResourceID: roleID, Relation: "member", SubjectType: "user", SubjectID: userID}, nil
}

func subjectFor(principalURN string) (subjectType, subjectID, subjectRelation string, err error) {
	kind, id, ok := strings.Cut(principalURN, ":")
	if !ok || id == "" {
		return "", "", "", fmt.Errorf("invalid principal %q", principalURN)
	}
	switch kind {
	case "user":
		if id == "all" {
			return "user", "*", "", nil
		}
		return "user", id, "", nil
	case "role":
		// role:<kind>:<id>; SpiceDB object IDs cannot hold ':'
		return "role", strings.ReplaceAll(id, ":", "_"), "member", nil
	case "agent", "api_key":
		return kind, id, "", nil
	default:
		// workload and system principals hold no grants
		return "", "", "", fmt.Errorf("principal %q cannot hold grants", principalURN)
	}
}

func (r *Registry) hasRelation(typeName, relation string) bool {
	def := r.Types[typeName]
	return def != nil && def.Relations[relation] != nil
}

func (d *RelationDef) caveatFor(subjectType string, wildcard bool, subjectRelation string) string {
	for _, s := range d.Subjects {
		if s.Type == subjectType && s.Wildcard == wildcard && s.Relation == subjectRelation {
			return s.Caveat
		}
	}
	return ""
}

func grantedParams(caveat *CaveatDef, selector map[string]string) map[string]any {
	params := map[string]any{}
	if caveat == nil {
		return params
	}
	for _, param := range caveat.GrantedParams {
		value := selector[strings.TrimPrefix(param, "granted_")]
		if value == "*" {
			value = ""
		}
		params[param] = value
	}
	return params
}
