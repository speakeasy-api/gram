package authzgen

import (
	"fmt"
	"go/format"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// refNames names the generated ref of each resource kind. A kind missing here
// is named after itself (agent becomes Agent).
var refNames = map[registry.Kind]string{"mcp": "MCPServer"}

// handWrittenRefs are the kinds whose requests carry dimensions a generic ref
// cannot fill in. Their constructors stay hand-written in authz.
var handWrittenRefs = map[registry.Kind]string{
	"mcp_auth_client": "MCPAuthClientCheck: client type, connection owner and upstream client come from the client row",
	"risk_policy":     "RiskPolicyApplies: evaluation pairs the policy with its bypass exemption",
	"inference":       "InferenceExecuteCheck: the project is both the resource and its parent",
}

// RefName is the Go name of a kind's ref constructor.
func RefName(kind registry.Kind) string {
	if name, ok := refNames[kind]; ok {
		return name
	}
	return camel(string(kind))
}

// IDTypeName is the Go name of a kind's id type: mcp becomes MCPServerID.
func IDTypeName(kind registry.Kind) string {
	return RefName(kind) + "ID"
}

// ActionMethodName is the Go name of the ref method for action:
// plugin_write becomes PluginWrite.
func ActionMethodName(action registry.Action) string {
	return camel(string(action))
}

func camel(s string) string {
	var b strings.Builder
	for _, word := range strings.Split(s, "_") {
		if word == "" {
			continue
		}
		if acronym, ok := identifierAcronyms[word]; ok {
			b.WriteString(acronym)
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	return b.String()
}

// paramName is the Go parameter for a dimension key: project_id becomes
// project.
func paramName(key string) string {
	name := strings.TrimSuffix(key, "_id")
	words := strings.Split(name, "_")
	for i := 1; i < len(words); i++ {
		words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
	}
	return strings.Join(words, "")
}

// paramType is the Go type of a dimension parameter: an id type for a key
// naming a kind (project_id is a ProjectID), otherwise a string.
func paramType(r registry.Registry, key string) string {
	if kind, ok := strings.CutSuffix(key, "_id"); ok && slices.ContainsFunc(r.Types, func(t registry.ResourceType) bool { return string(t.Kind) == kind }) {
		return IDTypeName(registry.Kind(kind))
	}
	return "string"
}

// refDimension is a dimension the ref fills in from its constructor: one
// naming another kind, such as project_id. Free-text and closed-set
// dimensions (tool, disposition) describe one use rather than the resource,
// so refs never take them.
func refDimension(r registry.Registry, d registry.Dimension) bool {
	return paramType(r, d.Key) != "string"
}

func renderRefs(r registry.Registry) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("package authz\n\n")
	b.WriteString("// Typed requests, one ref per resource kind and one method per permission.\n")
	b.WriteString("// A handler writes e.Require(ctx, authz.MCPServer(id, project).Write()), with\n// id and project built by the id types in ids_gen.go.\n")
	b.WriteString("// Kinds whose requests need hand-built dimensions are listed at the end.\n\n")

	for _, t := range r.Types {
		if _, ok := handWrittenRefs[t.Kind]; ok {
			for _, p := range t.Permissions {
				if len(p.DimensionSets) > 0 {
					return nil, fmt.Errorf("%s:%s declares dimension sets, but %s refs are hand-written, so no method would be generated", t.Kind, p.Action, t.Kind)
				}
			}
			continue
		}
		name := RefName(t.Kind)
		var dims []registry.Dimension
		for _, d := range t.Dimensions {
			if refDimension(r, d) {
				dims = append(dims, d)
			}
		}

		fmt.Fprintf(&b, "// %sRef names one %s resource for a request.\ntype %sRef struct {\n\tid %s\n", name, t.Kind, name, IDTypeName(t.Kind))
		for _, d := range dims {
			fmt.Fprintf(&b, "\t%s %s\n", paramName(d.Key), paramType(r, d.Key))
		}
		b.WriteString("}\n\n")

		params := []string{"id " + IDTypeName(t.Kind)}
		fields := []string{"id: id"}
		for _, d := range dims {
			params = append(params, paramName(d.Key)+" "+paramType(r, d.Key))
			fields = append(fields, paramName(d.Key)+": "+paramName(d.Key))
		}
		fmt.Fprintf(&b, "// %s names the %s resource id", name, t.Kind)
		for _, d := range dims {
			fmt.Fprintf(&b, ", in %s %s", strings.TrimSuffix(d.Key, "_id"), paramName(d.Key))
		}
		b.WriteString(".\n")
		fmt.Fprintf(&b, "func %s(%s) %sRef {\n\treturn %sRef{%s}\n}\n\n", name, strings.Join(params, ", "), name, name, strings.Join(fields, ", "))

		fmt.Fprintf(&b, "func (r %sRef) dimensions() map[string]string {\n", name)
		if len(dims) == 0 {
			b.WriteString("\treturn nil\n}\n\n")
		} else {
			b.WriteString("\treturn map[string]string{\n")
			for _, d := range dims {
				fmt.Fprintf(&b, "\t\t%q: r.%s.String(),\n", d.Key, paramName(d.Key))
			}
			b.WriteString("\t}\n}\n\n")
		}

		match := "MatchLoose"
		if t.Match == authz.MatchStrict {
			match = "MatchStrict"
		}
		for _, p := range t.Permissions {
			method := ActionMethodName(p.Action)
			var extra []registry.Dimension
			for _, d := range p.Dimensions {
				if refDimension(r, d) {
					extra = append(extra, d)
				}
			}
			fmt.Fprintf(&b, "// %s requests %s: %s\n", method, ScopeConstantName(t.ScopeFor(p)), p.Description)
			var mparams []string
			for _, d := range extra {
				mparams = append(mparams, paramName(d.Key)+" "+paramType(r, d.Key))
			}
			fmt.Fprintf(&b, "func (r %sRef) %s(%s) Check {\n", name, method, strings.Join(mparams, ", "))
			if len(extra) == 0 {
				fmt.Fprintf(&b, "\treturn newRefCheck(%s, r.id.String(), r.dimensions(), %s)\n}\n\n", ScopeConstantName(t.ScopeFor(p)), match)
				continue
			}
			b.WriteString("\tdimensions := r.dimensions()\n\tif dimensions == nil {\n\t\tdimensions = map[string]string{}\n\t}\n")
			for _, d := range extra {
				fmt.Fprintf(&b, "\tdimensions[%q] = %s.String()\n", d.Key, paramName(d.Key))
			}
			fmt.Fprintf(&b, "\treturn newRefCheck(%s, r.id.String(), dimensions, %s)\n}\n\n", ScopeConstantName(t.ScopeFor(p)), match)
		}
		for _, p := range t.Permissions {
			for _, set := range p.DimensionSets {
				for _, key := range set.Keys {
					if paramType(r, key) != "string" {
						return nil, fmt.Errorf("%s:%s dimension set %s uses %q, which the ref already fills in from its constructor", t.Kind, p.Action, set.Method, key)
					}
				}
				renderDimensionSet(&b, name, ScopeConstantName(t.ScopeFor(p)), match, set)
			}
		}
	}

	kinds := make([]string, 0, len(handWrittenRefs))
	for kind := range handWrittenRefs {
		kinds = append(kinds, string(kind))
	}
	slices.Sort(kinds)
	b.WriteString("// Kinds without a generated ref, and the hand-written constructor each uses:\n//\n")
	for _, kind := range kinds {
		fmt.Fprintf(&b, "//   - %s: %s\n", kind, handWrittenRefs[registry.Kind(kind)])
	}

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format refs_gen.go: %w\n%s", err, b.String())
	}
	return formatted, nil
}

// renderDimensionSet writes the ref method for one dimension set: the
// permission's check plus the set's dimensions as arguments, an empty one sent
// as unknown.
func renderDimensionSet(b *strings.Builder, ref, scope, match string, set registry.DimensionSet) {
	params := make([]string, 0, len(set.Keys))
	for _, key := range set.Keys {
		params = append(params, paramName(key))
	}
	fmt.Fprintf(b, "// %s requests %s with %s. An empty value is sent as unknown, so a\n", set.Method, scope, strings.Join(set.Keys, " and "))
	b.WriteString("// grant limited on that key never allows it and a block on it applies.\n")
	fmt.Fprintf(b, "func (r %sRef) %s(%s string) Check {\n", ref, set.Method, strings.Join(params, ", "))
	b.WriteString("\tdimensions := r.dimensions()\n\tif dimensions == nil {\n\t\tdimensions = map[string]string{}\n\t}\n")
	for i, key := range set.Keys {
		fmt.Fprintf(b, "\tdimensions[%q] = valueOrUnknown(%s)\n", key, params[i])
	}
	fmt.Fprintf(b, "\treturn newRefCheck(%s, r.id.String(), dimensions, %s)\n}\n\n", scope, match)
}

// renderIDs writes one id type per kind that has a generated ref. Each can
// only be built through its constructors, which keep an id that cannot name a
// resource as absent.
func renderIDs(r registry.Registry) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("package authz\n\nimport \"github.com/google/uuid\"\n\n")
	b.WriteString("// Id types, one per resource kind. A typed ref takes these, never a raw\n")
	b.WriteString("// string, so ids of different kinds cannot be swapped. An id built from an\n")
	b.WriteString("// empty or reserved value is absent, and every check built from it is refused.\n\n")
	for _, t := range r.Types {
		if _, ok := handWrittenRefs[t.Kind]; ok {
			continue
		}
		name := IDTypeName(t.Kind)
		fmt.Fprintf(&b, "// %s is the id of one %s resource in a check.\ntype %s struct{ id Option[string] }\n\n", name, t.Kind, name)
		fmt.Fprintf(&b, "// %sOf is the %s for a uuid. The nil uuid is absent.\nfunc %sOf(id uuid.UUID) %s { return %s{id: idFromUUID(id)} }\n\n", name, name, name, name, name)
		fmt.Fprintf(&b, "// %sFrom is the %s for an id held as a string. An empty, wildcard or\n// reserved value is absent.\nfunc %sFrom(id string) %s { return %s{id: idFromString(id)} }\n\n", name, name, name, name, name)
		fmt.Fprintf(&b, "// String is the id as checks and grants carry it, empty when absent.\nfunc (i %s) String() string { return idValue(i.id) }\n\n", name)
		fmt.Fprintf(&b, "// Equals reports whether both ids name the same %s. It fails with\n// ErrEmptyValue when either is absent.\nfunc (i %s) Equals(other %s) (bool, error) { return i.id.Equals(other.id) }\n\n", t.Kind, name, name)
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format ids_gen.go: %w\n%s", err, b.String())
	}
	return formatted, nil
}
