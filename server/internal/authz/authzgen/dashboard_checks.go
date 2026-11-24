package authzgen

import (
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/authz/internal/registry"
)

// The dashboard gets one check constructor per Go ref and collection, with the
// same name, arguments and methods: authz.mcpServer(id, projectId).write() is
// authz.MCPServer(id, project).Write(). Tool calls (DimensionSets such as
// ToolCall) are left out: the dashboard never decides one. Both sides are rendered from
// dashboardConstructors, and the cases in authzcases are answered by calling
// the Go function each TypeScript constructor names, so a constructor that
// asks a different question than its Go twin fails the dashboard's tests.

const dashboardCaseConstructorsPath = "internal/authz/authzcases/constructors_gen.go"

// dashboardParam is one argument of a constructor or method.
type dashboardParam struct {
	// Name is the TypeScript parameter: id, projectId, orgId.
	Name string
	// IDType is the Go id type the argument is built as: ProjectID.
	IDType string
	// Key is the selector key the argument fills in, empty for the resource id.
	Key string
}

type dashboardMethod struct {
	Name   string
	GoName string
	Scope  registry.Scope
	Label  string
	Params []dashboardParam
	// Connection marks a check the Go method stamps as a connection, which
	// may leave out the keys the permission opens on one.
	Connection bool
}

type dashboardConstructor struct {
	Name   string
	GoName string
	Kind   registry.Kind
	Params []dashboardParam
	// Collection is a constructor naming every resource of the kind in one
	// parent, whose id is the check's resource.
	Collection bool
	// Ref is the constructor naming one resource of the same kind, for a
	// collection.
	Ref string
	// InProject marks a collection whose resources carry their project, so
	// its checks carry the parent as the project too.
	InProject bool
	Methods   []dashboardMethod
}

// tsName lowers the first letter of a Go name: MCPServer becomes mcpServer.
func tsName(goName string) string {
	upper := 0
	for upper < len(goName) && goName[upper] >= 'A' && goName[upper] <= 'Z' {
		upper++
	}
	switch {
	case upper == 0:
		return goName
	case upper == 1 || upper == len(goName):
		return strings.ToLower(goName[:upper]) + goName[upper:]
	default:
		// An acronym ends one letter before the next word: MCPServer is
		// MCP + Server.
		return strings.ToLower(goName[:upper-1]) + goName[upper-1:]
	}
}

// tsParamName is the TypeScript parameter for a dimension key: project_id
// becomes projectId.
func tsParamName(key string) string {
	return paramName(key) + "Id"
}

// selectorKeyName is a selector key as the API spells it: project_id becomes
// projectId.
func selectorKeyName(key string) string {
	words := strings.Split(key, "_")
	for i := 1; i < len(words); i++ {
		words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
	}
	return strings.Join(words, "")
}

func dashboardConstructors(r registry.Registry) []dashboardConstructor {
	var out []dashboardConstructor
	for _, t := range r.Types {
		if _, ok := handWrittenRefs[t.Kind]; ok {
			continue
		}
		name := RefName(t.Kind)
		ref := dashboardConstructor{Name: tsName(name), GoName: name, Kind: t.Kind, Params: []dashboardParam{{Name: "id", IDType: IDTypeName(t.Kind), Key: ""}}, Collection: false, Ref: "", InProject: false, Methods: nil}
		for _, d := range t.Dimensions {
			if refDimension(r, d) {
				ref.Params = append(ref.Params, dashboardParam{Name: tsParamName(d.Key), IDType: paramType(r, d.Key), Key: d.Key})
			}
		}
		for _, p := range t.Permissions {
			m := dashboardMethod{
				Name:       tsName(ActionMethodName(p.Action)),
				GoName:     ActionMethodName(p.Action),
				Scope:      t.ScopeFor(p),
				Label:      p.Label,
				Params:     nil,
				Connection: slices.ContainsFunc(p.Open, func(o registry.OpenKeys) bool { return o.On == registry.OpenOnConnection }),
			}
			for _, d := range p.Dimensions {
				if refDimension(r, d) {
					m.Params = append(m.Params, dashboardParam{Name: tsParamName(d.Key), IDType: paramType(r, d.Key), Key: d.Key})
				}
			}
			ref.Methods = append(ref.Methods, m)
		}
		out = append(out, ref)

		if !hasCollection(r, t) {
			continue
		}
		parent, parentType := "projectId", "ProjectID"
		if t.Parent == "org" {
			parent, parentType = "orgId", "OrgID"
		}
		coll := dashboardConstructor{Name: tsName(CollectionName(t.Kind)), GoName: CollectionName(t.Kind), Kind: t.Kind, Params: []dashboardParam{{Name: parent, IDType: parentType, Key: ""}}, Collection: true, Ref: ref.Name, InProject: slices.ContainsFunc(ref.Params, func(p dashboardParam) bool { return p.Key == "project_id" }), Methods: nil}
		for _, m := range ref.Methods {
			if len(m.Params) == 0 {
				coll.Methods = append(coll.Methods, m)
			}
		}
		out = append(out, coll)
	}
	return out
}

// checkDimensions is the TypeScript object of the dimensions a method's check
// carries, in the API's spelling: { projectId: projectId }.
func checkDimensions(c dashboardConstructor, m dashboardMethod) string {
	var fields []string
	params := c.Params
	if c.Collection {
		if c.InProject {
			fields = append(fields, "projectId: "+c.Params[0].Name)
		}
		params = nil
	}
	for _, p := range append(slices.Clone(params), m.Params...) {
		if p.Key != "" {
			fields = append(fields, selectorKeyName(p.Key)+": "+p.Name)
		}
	}
	if len(fields) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(fields, ", ") + " }"
}

const dashboardCheckPreamble = `// Assigned in Check's static block, so only this module builds checks.
let newCheck: (
  scope: Scope,
  resourceKind: string,
  resourceId: string,
  dimensions: Readonly<Record<string, string>>,
  ids: readonly string[],
  options?: CheckOptions,
) => Check;

interface CheckOptions {
  readonly connection?: boolean;
  readonly collection?: boolean;
}

/**
 * One permission question, built by a constructor below and never by hand.
 * Each constructor is the Go ref of the same name (authz.mcpServer(id,
 * projectId).write() is authz.MCPServer(id, project).Write()), so every
 * argument the server needs is required. An id that cannot name a resource
 * (empty, "*", a "$" marker, or containing whitespace) makes the check
 * invalid, and an invalid check is never allowed.
 */
export class Check {
  readonly scope: Scope;
  readonly resourceKind: string;
  readonly resourceId: string;
  /** Dimensions the check carries, in the API's spelling (projectId). */
  readonly dimensions: Readonly<Record<string, string>>;
  /** Built as a connection, which names no tool. */
  readonly connection: boolean;
  /** Names every resource of its kind in one parent. */
  readonly collection: boolean;
  readonly invalid: boolean;
  readonly #probe: boolean;

  private constructor(fields: CheckFields, probe: boolean) {
    this.scope = fields.scope;
    this.resourceKind = fields.resourceKind;
    this.resourceId = fields.resourceId;
    this.dimensions = fields.dimensions;
    this.connection = fields.connection;
    this.collection = fields.collection;
    this.invalid = fields.invalid;
    this.#probe = probe;
  }

  static {
    newCheck = (
      scope: Scope,
      resourceKind: string,
      resourceId: string,
      dimensions: Readonly<Record<string, string>>,
      ids: readonly string[],
      options?: CheckOptions,
    ): Check =>
      new Check(
        {
          scope,
          resourceKind,
          resourceId,
          dimensions,
          connection: options?.connection ?? false,
          collection: options?.collection ?? false,
          invalid: ids.some((id) => !namesResource(id)),
        },
        false,
      );
  }

  get isProbe(): boolean {
    return this.#probe;
  }

  /**
   * The discovery question: could the caller act on some resource matching
   * the check. On one resource it may leave out the keys its permission
   * opens on a probe, as Check.Probe does on the server. On a collection it
   * asks about any resource of the kind in that parent, which the server
   * answers by listing them.
   */
  probe(): Check {
    return new Check(this, true);
  }
}

interface CheckFields {
  readonly scope: Scope;
  readonly resourceKind: string;
  readonly resourceId: string;
  readonly dimensions: Readonly<Record<string, string>>;
  readonly connection: boolean;
  readonly collection: boolean;
  readonly invalid: boolean;
}

/** Mirrors idFromString in authz/ids.go, spaces as Go's unicode.IsSpace. */
function namesResource(id: string): boolean {
  return (
    id !== "" &&
    id.toLowerCase() !== "00000000-0000-0000-0000-000000000000" &&
    id !== "*" &&
    !id.startsWith("$") &&
    !/[SPACES]/.test(id)
  );
}

`

// renderDashboardChecks writes the TypeScript constructors and the open keys
// table the dashboard's matcher reads.
func renderDashboardChecks(b *strings.Builder, r registry.Registry) {
	constructors := dashboardConstructors(r)

	b.WriteString("/**\n * The grant keys a probe or a connection may leave out and still match a\n * grant limited on them, per scope, in the API's spelling.\n */\n")
	b.WriteString("export const scopeOpenKeys: Readonly<\n  Record<string, { readonly probe: readonly string[]; readonly connection: readonly string[] }>\n> = {\n")
	for _, s := range r.Scopes() {
		var probe, connection []string
		for _, o := range s.Permission.Open {
			for _, key := range o.Keys {
				switch o.On {
				case registry.OpenOnProbe:
					probe = append(probe, strconv.Quote(selectorKeyName(key)))
				case registry.OpenOnConnection:
					connection = append(connection, strconv.Quote(selectorKeyName(key)))
				case registry.OpenOnPolicyAudience:
				}
			}
		}
		if s.Block || (len(probe) == 0 && len(connection) == 0) {
			continue
		}
		fmt.Fprintf(b, "  %s: { probe: [%s], connection: [%s] },\n", strconv.Quote(string(s.Scope)), strings.Join(probe, ", "), strings.Join(connection, ", "))
	}
	b.WriteString("};\n\n")

	b.WriteString(strings.Replace(dashboardCheckPreamble, "SPACES", tsSpaceClass(), 1))
	for _, c := range constructors {
		params := make([]string, 0, len(c.Params))
		args := make([]string, 0, len(c.Params))
		for _, p := range c.Params {
			params = append(params, p.Name+": string")
			args = append(args, p.Name)
		}
		goArgs := make([]string, 0, len(c.Params))
		for _, p := range c.Params {
			goArgs = append(goArgs, strings.TrimSuffix(p.Name, "Id"))
		}
		if c.Collection {
			fmt.Fprintf(b, "/** Every %s resource of one parent: authz.%s(%s). */\n", c.Kind, c.GoName, strings.Join(goArgs, ", "))
		} else {
			fmt.Fprintf(b, "/** One %s resource: authz.%s(%s). */\n", c.Kind, c.GoName, strings.Join(goArgs, ", "))
		}
		methodTypes := make([]string, 0, len(c.Methods))
		for _, m := range c.Methods {
			mparams := make([]string, 0, len(m.Params))
			for _, p := range m.Params {
				mparams = append(mparams, p.Name+": string")
			}
			methodTypes = append(methodTypes, fmt.Sprintf("  readonly %s: (%s) => Check;\n", m.Name, strings.Join(mparams, ", ")))
		}
		fmt.Fprintf(b, "export function %s(%s): {\n%s} {\n  return {\n", c.Name, strings.Join(params, ", "), strings.Join(methodTypes, ""))
		for _, m := range c.Methods {
			mparams := make([]string, 0, len(m.Params))
			ids := slices.Clone(args)
			for _, p := range m.Params {
				mparams = append(mparams, p.Name+": string")
				ids = append(ids, p.Name)
			}
			var options []string
			if m.Connection {
				options = append(options, "connection: true")
			}
			if c.Collection {
				options = append(options, "collection: true")
			}
			opt := ""
			if len(options) > 0 {
				opt = ", { " + strings.Join(options, ", ") + " }"
			}
			fmt.Fprintf(b, "    /** %s: %s */\n", m.Scope, m.Label)
			fmt.Fprintf(b, "    %s: (%s): Check =>\n      newCheck(%s, %s, %s, %s, [%s]%s),\n",
				m.Name, strings.Join(mparams, ", "),
				strconv.Quote(string(m.Scope)), strconv.Quote(string(c.Kind)), c.Params[0].Name,
				checkDimensions(c, m), strings.Join(ids, ", "), opt)
		}
		b.WriteString("  };\n}\n\n")
	}
}

// renderDashboardCaseConstructors writes, for authzcases, the Go function
// behind each TypeScript constructor method, keyed the way the cases name
// them: "mcpServer.write".
func renderDashboardCaseConstructors(r registry.Registry) ([]byte, error) {
	constructors := dashboardConstructors(r)
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("package authzcases\n\nimport \"github.com/speakeasy-api/gram/server/internal/authz\"\n\n")
	b.WriteString("// constructors holds the Go twin of every dashboard check constructor\n// method. build takes the constructor's arguments, then the method's.\n")
	b.WriteString("var constructors = []constructor{\n")
	for _, c := range constructors {
		for _, m := range c.Methods {
			var cargs, margs, keys []string
			for i, p := range c.Params {
				cargs = append(cargs, fmt.Sprintf("authz.%sFrom(args[%d])", p.IDType, i))
				keys = append(keys, strconv.Quote(p.Key))
			}
			for i, p := range m.Params {
				margs = append(margs, fmt.Sprintf("authz.%sFrom(args[%d])", p.IDType, len(c.Params)+i))
				keys = append(keys, strconv.Quote(p.Key))
			}
			parent := ""
			if c.Collection {
				parent = strings.TrimSuffix(c.Params[0].Name, "Id")
			}
			fmt.Fprintf(&b, "\t{name: %s, method: %s, scope: %s, parent: %s, ref: %s, keys: []string{%s}, build: func(args []string) authz.Check { return authz.%s(%s).%s(%s) }},\n",
				strconv.Quote(c.Name), strconv.Quote(m.Name), strconv.Quote(string(m.Scope)), strconv.Quote(parent), strconv.Quote(c.Ref), strings.Join(keys, ", "),
				c.GoName, strings.Join(cargs, ", "), m.GoName, strings.Join(margs, ", "))
		}
	}
	b.WriteString("}\n")
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("format %s: %w\n%s", dashboardCaseConstructorsPath, err, b.String())
	}
	return formatted, nil
}

// tsSpaceClass is the body of a regular expression character class holding
// every rune unicode.IsSpace accepts. JavaScript's \s differs: it has U+FEFF
// and lacks U+0085.
func tsSpaceClass() string {
	var b strings.Builder
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.IsSpace(r) {
			continue
		}
		end := r
		for end+1 <= unicode.MaxRune && unicode.IsSpace(end+1) {
			end++
		}
		fmt.Fprintf(&b, "\\u%04x", r)
		if end > r {
			fmt.Fprintf(&b, "-\\u%04x", end)
		}
		r = end
	}
	return b.String()
}
