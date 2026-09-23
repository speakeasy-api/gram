package remotesessions

import (
	"encoding/json"
	"errors"
	"strconv"
)

// jsonInt accepts JSON integers and quoted integers from upstream providers.
// Its underlying integer type preserves numeric encoding when marshaled.
type jsonInt int

func (i *jsonInt) UnmarshalJSON(data []byte) error {
	var value json.Number
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("expected a JSON integer or quoted integer")
	}
	// As with encoding/json's integer decoder, null leaves the value unchanged.
	if value == "" {
		return nil
	}
	n, err := strconv.Atoi(string(value))
	if err != nil {
		return errors.New("JSON integer must be within int range")
	}
	*i = jsonInt(n)
	return nil
}
