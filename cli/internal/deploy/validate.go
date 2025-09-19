package deploy

import (
	"fmt"
)

// failDuplicates returns an error if any values in the counter map appear more than once.
func failDuplicates(counter map[string]int, errorPrefix string) error {
	var duplicates []string
	for key, count := range counter {
		if count > 1 {
			duplicates = append(duplicates, fmt.Sprintf("'%s' (%d times)", key, count))
		}
	}

	if len(duplicates) > 0 {
		return fmt.Errorf("%s: %v", errorPrefix, duplicates)
	}
	return nil
}

// validateUniqueNames returns an error if source names are not unique.
func validateUniqueNames(sources []Source) error {
	counter := make(map[string]int)
	for _, source := range sources {
		counter[source.Name]++
	}

	return failDuplicates(counter, "source names must be unique")
}

// validateUniqueSlugs returns an error if source slugs are not unique.
func validateUniqueSlugs(sources []Source) error {
	counter := make(map[string]int)
	for _, source := range sources {
		counter[source.Slug]++
	}

	return failDuplicates(counter, "source slugs must be unique")
}
