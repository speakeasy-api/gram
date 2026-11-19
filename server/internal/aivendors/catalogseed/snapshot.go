package catalogseed

// snapshot owns the frozen file format independently of evolving runtime types.
type snapshot struct {
	// Notice records the ownership and maintenance policy in the embedded file.
	Notice string `json:"Notice"`

	// Products contains every product, alias and document at bootstrap time.
	Products []product `json:"Products"`
}

type product struct {
	// ID is the stable scan and decision identifier.
	ID string `json:"ID"`

	// VendorKey groups products from the same vendor.
	VendorKey string `json:"VendorKey"`

	// Family groups a vendor's related products; empty means its own family.
	Family string `json:"Family"`

	// DisplayName is the initial product label.
	DisplayName string `json:"DisplayName"`

	// Category classifies scanned products; empty means OAuth-only.
	Category string `json:"Category"`

	// Signatures contains the on-device detection footprints.
	Signatures signatures `json:"Signatures"`

	// VersionPlistKey overrides the default macOS version key when nonempty.
	VersionPlistKey string `json:"VersionPlistKey"`

	// ClientInfoNames contains self-reported MCP names for attribution only.
	ClientInfoNames []string `json:"ClientInfoNames"`

	// Aliases contains other reported names for attribution only.
	Aliases []alias `json:"Aliases"`

	// Documents contains the product's exact URLs and wildcard patterns.
	Documents []document `json:"Documents"`
}

type signatures struct {
	// BundleIDs identifies macOS application bundles.
	BundleIDs []string `json:"BundleIDs"`

	// Binaries contains executable names resolved on the device PATH.
	Binaries []string `json:"Binaries"`

	// ConfigDirs contains directories that indicate an installed product.
	ConfigDirs []string `json:"ConfigDirs"`

	// ProcessNames contains names that indicate a running product.
	ProcessNames []string `json:"ProcessNames"`
}

type document struct {
	// URL is an exact client ID or a pattern containing whole-segment wildcards.
	URL string `json:"URL"`

	// DisplayName is the initial document label.
	DisplayName string `json:"DisplayName"`

	// Enabled records whether this document initially admits clients.
	Enabled bool `json:"Enabled"`
}

type alias struct {
	// Name is the reported name, stored as written.
	Name string `json:"Name"`

	// Client is the kind of client the name implies; empty means none.
	Client string `json:"Client"`
}
