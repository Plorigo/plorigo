// Package id provides the domain identifier type, decoupling domain structs and
// proto/JSON (strings) from the database column type.
package id

import "github.com/google/uuid"

// ID is an opaque identifier rendered as a UUID string.
type ID string

// New returns a fresh random ID.
func New() ID { return ID(uuid.NewString()) }

// Parse validates that s is a UUID and returns it as an ID.
func Parse(s string) (ID, error) {
	if _, err := uuid.Parse(s); err != nil {
		return "", err
	}
	return ID(s), nil
}

func (i ID) String() string { return string(i) }
