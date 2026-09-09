package remotesessions

// WaitIdentityRestatements blocks until every detached identity restatement has finished.
func (s *RefreshService) WaitIdentityRestatements() { s.restatements.Wait() }

func (m *ChallengeManager) WaitIdentityRestatements() { m.refresher.WaitIdentityRestatements() }
