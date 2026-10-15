package demoseed

import "context"

// postflight: Postflight asserts: demo data landed, and nothing leaked outside the demo org.
func (s *seeder) postflight(ctx context.Context) error {
	// TODO(demoseed-go): convert from postgres.sql.
	return nil
}
