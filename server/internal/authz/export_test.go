package authz

// WaitForPublishDrains blocks until the drain pool has observed every publish
// result enqueued by PublishChallenge so far. Test-only synchronization
// barrier: callers must have already returned from the PublishChallenge whose
// drain they await.
func (p *ChallengePublisher) WaitForPublishDrains() {
	p.drainer.Wait()
}
