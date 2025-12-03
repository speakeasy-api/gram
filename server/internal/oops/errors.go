package oops

import "fmt"

func Prefix(err error, prefix string) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", prefix, err)
}
