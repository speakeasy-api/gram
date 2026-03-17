package types

type RegistrySource string

const (
	RegistrySourceInternal RegistrySource = "internal"
	RegistrySourceExternal RegistrySource = "external"
)

func (s RegistrySource) Valid() bool {
	return s == RegistrySourceInternal || s == RegistrySourceExternal
}

func (s RegistrySource) String() string {
	return string(s)
}
