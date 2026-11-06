package assets

// ValidateCatalogSVG validates a portable catalog logo before any bundle writes.
func ValidateCatalogSVG(data []byte) error {
	_, err := sanitizeSVG(data)
	return err
}
