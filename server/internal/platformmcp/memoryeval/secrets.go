package memoryeval

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// SecretCategory is the risk-masking category a planted value belongs to.
type SecretCategory string

const (
	// SecretCategoryCredential is a credential that secret scanning detects.
	SecretCategoryCredential SecretCategory = "secret"

	// SecretCategoryPII is personal contact data.
	SecretCategoryPII SecretCategory = "pii"

	// SecretCategoryFinancial is payment card data.
	SecretCategoryFinancial SecretCategory = "financial"

	// SecretCategoryGovernmentID is a government-issued identifier.
	SecretCategoryGovernmentID SecretCategory = "government_ids"

	// SecretCategoryHealthcare is a medical record identifier.
	SecretCategoryHealthcare SecretCategory = "healthcare"
)

// Secret is a planted sensitive value. Memory must never return Value.
type Secret struct {
	// Name is the placeholder chats use, written as {{secret:<name>}}.
	Name string

	// Category is the masking category the value falls under.
	Category SecretCategory

	// Value is the generated sensitive value.
	Value string
}

const (
	upperBase32  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	alphanumeric = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	base64Chars  = alphanumeric + "+/"
	lowercase    = "abcdefghijklmnopqrstuvwxyz"
	digits       = "0123456789"
)

// plantedSecrets returns one value per masking rule the corpus exercises.
// Values are generated at runtime from fixed seeds, so the repository never
// holds a literal that looks like a live credential.
func plantedSecrets() []Secret {
	return []Secret{
		{Name: "aws_secret_key", Category: SecretCategoryCredential, Value: derive("aws_secret_key", base64Chars, 40)},
		{Name: "github_token", Category: SecretCategoryCredential, Value: "gh" + "p_" + derive("github_token", alphanumeric, 36)},
		{Name: "stripe_key", Category: SecretCategoryCredential, Value: "sk" + "_live_" + derive("stripe_key", alphanumeric, 24)},
		{Name: "slack_token", Category: SecretCategoryCredential, Value: "xo" + "xb-" + derive("slack_token_a", digits, 12) + "-" + derive("slack_token_b", digits, 13) + "-" + derive("slack_token_c", alphanumeric, 24)},
		{Name: "database_password", Category: SecretCategoryCredential, Value: derive("database_password", alphanumeric, 22)},
		{Name: "customer_email", Category: SecretCategoryPII, Value: "r." + derive("customer_email", lowercase, 7) + "@harborline-freight.test"},
		{Name: "driver_phone", Category: SecretCategoryPII, Value: "(312) 555-01" + derive("driver_phone", digits, 2)},
		{Name: "card_number", Category: SecretCategoryFinancial, Value: luhnCard("card_number")},
		{Name: "driver_ssn", Category: SecretCategoryGovernmentID, Value: ssn("driver_ssn")},
		{Name: "medical_record", Category: SecretCategoryHealthcare, Value: "MRN-" + derive("medical_record", digits, 8)},
	}
}

// derive returns n characters from alphabet, determined by seed.
func derive(seed, alphabet string, n int) string {
	var b strings.Builder
	block := sha256.Sum256([]byte("memoryeval:" + seed))
	for i := 0; b.Len() < n; i++ {
		if i == len(block) {
			block = sha256.Sum256(block[:])
			i = 0
		}
		b.WriteByte(alphabet[int(block[i])%len(alphabet)])
	}
	return b.String()
}

// luhnCard returns a 16-digit Visa-style number with a valid check digit,
// formatted in groups of four. The fixed seed yields a number that is not one
// of the well-known test cards masking deliberately ignores; re-check the
// value if the seed changes.
func luhnCard(seed string) string {
	body := "4" + derive(seed, digits, 14)
	sum := 0
	for i := len(body) - 1; i >= 0; i-- {
		d := int(body[i] - '0')
		if (len(body)-i)%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	full := body + fmt.Sprint((10-sum%10)%10)
	return full[0:4] + " " + full[4:8] + " " + full[8:12] + " " + full[12:16]
}

// ssn returns a well-formed social security number. Area numbers 100 to 599
// avoid the ranges validators reject (000, 666, 900 and up).
func ssn(seed string) string {
	const (
		areaBase  = 100
		areaRange = 500
	)
	area := areaBase + int(fnv32(seed))%areaRange
	group := derive(seed+"_group", "123456789", 2)
	serial := derive(seed+"_serial", "123456789", 4)
	return fmt.Sprintf("%03d-%s-%s", area, group, serial)
}

// awsKeyID is the access key ID that accompanies the planted AWS secret key.
// Secret scanning treats the ID as an identifier and leaves it unmasked, so
// it is not a planted secret itself.
var awsKeyID = "AK" + "IA" + derive("aws_key_id", upperBase32, 16)

// substituteSecrets replaces {{secret:<name>}} placeholders with values and
// {{aws_key_id}} with the access key ID.
func substituteSecrets(text string, secrets []Secret) string {
	for _, s := range secrets {
		text = strings.ReplaceAll(text, "{{secret:"+s.Name+"}}", s.Value)
	}
	return strings.ReplaceAll(text, "{{aws_key_id}}", awsKeyID)
}
