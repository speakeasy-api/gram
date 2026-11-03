package mcpversions

import "slices"

// MCP extensions are negotiated separately from the protocol revision: a
// server declares each one under `capabilities.extensions` keyed by its
// reverse-domain label, and the methods an extension defines are available
// whenever the server declares it, on any revision the server serves. No
// revision's core protocol defines them, so [DefinesMethod] never reports
// them; a surface that implements an extension dispatches its methods on the
// strength of the capability declaration instead.
const (
	// ExtensionSkills is the capability label of the Skills extension
	// (SEP-2640), which serves Agent Skills over the Resources primitive.
	ExtensionSkills = "io.modelcontextprotocol/skills"

	// MethodSkillsList lists the skills a server serves, with pagination.
	// Defined by [ExtensionSkills].
	MethodSkillsList = "skills/list"

	// MethodSkillsGet retrieves one skill entry by the URI of its SKILL.md.
	// Defined by [ExtensionSkills].
	MethodSkillsGet = "skills/get"

	// MethodResourcesDirectoryRead lists the resources directly under one
	// directory URI. Defined by [ExtensionSkills], gated behind its
	// `directoryRead` setting.
	MethodResourcesDirectoryRead = "resources/directory/read"
)

// extensionMethods maps every client-to-server method a recognized extension
// defines to the extension's capability label. Like clientToServerMethods it
// is a statement about the specification, not about Speakeasy: recognizing a
// method here lets the request census count demand for it before any surface
// serves it, and each surface's dispatch still decides what it implements.
var extensionMethods = map[string]string{
	MethodSkillsList:             ExtensionSkills,
	MethodSkillsGet:              ExtensionSkills,
	MethodResourcesDirectoryRead: ExtensionSkills,
}

// KnownExtensionMethod reports whether a recognized extension defines the
// client-to-server method. A method the core protocol defines in any revision
// is not an extension method, even when a later revision moved it into one.
func KnownExtensionMethod(method string) bool {
	_, ok := extensionMethods[method]
	return ok
}

// ExtensionMethods returns every client-to-server method a recognized
// extension defines, sorted so the result is stable for tests and listings.
func ExtensionMethods() []string {
	methods := make([]string, 0, len(extensionMethods))
	for method := range extensionMethods {
		methods = append(methods, method)
	}
	slices.Sort(methods)
	return methods
}

// MethodExtension returns the capability label of the extension defining the
// client-to-server method, or "" when no recognized extension defines it.
func MethodExtension(method string) string {
	return extensionMethods[method]
}
