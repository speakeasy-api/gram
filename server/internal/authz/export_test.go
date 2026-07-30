package authz

// WaitForPublishDrains blocks until every ack-drain goroutine spawned by
// PublishChallenge so far has finished. Test-only synchronization barrier.
func (p *ChallengePublisher) WaitForPublishDrains() {
	p.drains.Wait()
}
