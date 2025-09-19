package secret

type Secret string

func (s Secret) String() string {
	return "<redacted>"
}

func (s Secret) Reveal() string {
	return string(s)
}
