package urn

import "database/sql/driver"

const directoryHandoffPrefix = "directory_handoff"

type DirectoryHandoff struct {
	ID string
}

func NewDirectoryHandoff(organizationID string) DirectoryHandoff {
	return DirectoryHandoff{ID: organizationID}
}

func ParseDirectoryHandoff(value string) (DirectoryHandoff, error) {
	id, err := settingsURNParse(directoryHandoffPrefix, value)
	if err != nil {
		return DirectoryHandoff{}, err
	}
	return DirectoryHandoff{ID: id}, nil
}

func (u DirectoryHandoff) IsZero() bool {
	return u.ID == ""
}

func (u DirectoryHandoff) String() string {
	return settingsURNString(directoryHandoffPrefix, u.ID)
}

func (u DirectoryHandoff) MarshalJSON() ([]byte, error) {
	return settingsURNMarshalJSON(directoryHandoffPrefix, u.ID)
}

func (u *DirectoryHandoff) UnmarshalJSON(data []byte) error {
	id, err := settingsURNUnmarshalJSON(directoryHandoffPrefix, data)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u *DirectoryHandoff) Scan(value any) error {
	if value == nil {
		return nil
	}
	id, err := settingsURNScan(directoryHandoffPrefix, "DirectoryHandoff", value)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}

func (u DirectoryHandoff) Value() (driver.Value, error) {
	return settingsURNValue(directoryHandoffPrefix, u.ID)
}

func (u DirectoryHandoff) MarshalText() ([]byte, error) {
	return settingsURNMarshalText(directoryHandoffPrefix, u.ID)
}

func (u *DirectoryHandoff) UnmarshalText(text []byte) error {
	id, err := settingsURNUnmarshalText(directoryHandoffPrefix, text)
	if err != nil {
		return err
	}
	u.ID = id
	return nil
}
