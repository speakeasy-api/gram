package slackdirectoryconnections

// OccupySyncSlotsForTest takes every sync slot and returns a function that frees them.
func (s *DirectorySync) OccupySyncSlotsForTest() func() {
	for range cap(s.slots) {
		s.slots <- struct{}{}
	}
	return func() {
		for range cap(s.slots) {
			<-s.slots
		}
	}
}
