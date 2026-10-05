package widgets

// RequestDecoder exposes the service's request body decoder to tests.
var RequestDecoder = requestDecoder

// UsePresets serves the given presets file in place of the embedded one.
func UsePresets(s *Service, raw []byte) {
	s.presets = func() (map[string]presetPage, error) { return parsePresets(raw) }
}
