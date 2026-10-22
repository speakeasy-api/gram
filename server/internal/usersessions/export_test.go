package usersessions

// SetHostedSync replaces the per-wrapper sync the issuer migration runs.
func (s *Service) SetHostedSync(sync HostedSyncFunc) { s.hostedSync = sync }
