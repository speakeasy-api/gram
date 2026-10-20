package assets

import "time"

// SetClockForTest lets tests move an issued upload token past its expiry.
func (u *OrganizationImageUploads) SetClockForTest(now func() time.Time) {
	u.now = now
}
