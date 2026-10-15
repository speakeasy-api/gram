package demoseed

import "context"

// postflight asserts the demo data landed and nothing leaked outside the demo
// org. Any failure rolls the whole Postgres seed back.
func (s *seeder) postflight(ctx context.Context) error {
	if err := s.postflightCore(ctx); err != nil {
		return err
	}
	return s.postflightIdentity(ctx)
}

// postflightCore covers postgres.sql lines 3139-3693.
func (s *seeder) postflightCore(ctx context.Context) error {
	// TODO(demoseed-go): convert from postgres.sql.
	return nil
}
