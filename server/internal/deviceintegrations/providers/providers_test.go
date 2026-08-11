package providers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeSource struct{}

func (fakeSource) TestConnection(ctx context.Context, creds Credentials, settings Settings) error {
	return nil
}

func (fakeSource) ListDevices(ctx context.Context, creds Credentials, settings Settings, cursor string) (DevicePage, error) {
	return DevicePage{Devices: nil, NextCursor: ""}, nil
}

func validDescriptor(id string) Descriptor {
	return Descriptor{
		ID:           id,
		DisplayName:  "Fake " + id,
		Capabilities: []Capability{CapabilityInventorySource},
		Fields: []CredentialField{
			{Key: "url", Label: "URL", Kind: FieldKindURL, Secret: false, Required: true},
			{Key: "token", Label: "Token", Kind: FieldKindText, Secret: true, Required: true},
		},
		Schedules: []ScheduleSpec{
			{Schedule: id + "_inventory", Capability: CapabilityInventorySource, Interval: time.Hour},
		},
		NewInventorySource: func(deps Deps) InventorySource { return fakeSource{} },
		NewEvidenceSink:    nil,
	}
}

func TestRegisterAndLookup(t *testing.T) {
	t.Parallel()

	Register(validDescriptor("fake_lookup"))

	d, ok := Lookup("fake_lookup")
	require.True(t, ok)
	require.Equal(t, "Fake fake_lookup", d.DisplayName)

	_, ok = Lookup("nope")
	require.False(t, ok)
}

func TestRegisterRejectsDuplicates(t *testing.T) {
	t.Parallel()

	Register(validDescriptor("fake_dup"))
	require.Panics(t, func() { Register(validDescriptor("fake_dup")) })
}

func TestRegisterRejectsCapabilityConstructorMismatch(t *testing.T) {
	t.Parallel()

	// Capability declared without a constructor.
	broken := validDescriptor("fake_nosource")
	broken.NewInventorySource = nil
	require.Panics(t, func() { Register(broken) })

	// Constructor supplied without the capability.
	broken2 := validDescriptor("fake_nocap")
	broken2.Capabilities = []Capability{CapabilityEvidenceSink}
	require.Panics(t, func() { Register(broken2) })
}

func TestRegisterRejectsDuplicateFieldsAndSchedules(t *testing.T) {
	t.Parallel()

	dupField := validDescriptor("fake_dupfield")
	dupField.Fields = append(dupField.Fields, CredentialField{Key: "url", Label: "URL again", Kind: FieldKindText, Secret: false, Required: false})
	require.Panics(t, func() { Register(dupField) })

	dupSched := validDescriptor("fake_dupsched")
	dupSched.Schedules = append(dupSched.Schedules, dupSched.Schedules[0])
	require.Panics(t, func() { Register(dupSched) })
}

func TestFieldRouting(t *testing.T) {
	t.Parallel()

	d := validDescriptor("fake_routing")
	secrets := d.SecretFields()
	require.Len(t, secrets, 1)
	require.Equal(t, "token", secrets[0].Key)

}

func TestRegisterRejectsScheduleCapabilityMismatches(t *testing.T) {
	t.Parallel()

	// Schedule naming a capability the provider does not declare.
	undeclared := validDescriptor("fake_undeclared_cap")
	undeclared.Schedules[0].Capability = CapabilityEvidenceSink
	require.Panics(t, func() { Register(undeclared) })

	// Two schedules driving the same capability would run the identical
	// pipeline twice.
	doubled := validDescriptor("fake_doubled_cap")
	doubled.Schedules = append(doubled.Schedules, ScheduleSpec{
		Schedule: "fake_doubled_cap_second", Capability: CapabilityInventorySource, Interval: time.Hour,
	})
	require.Panics(t, func() { Register(doubled) })
}
