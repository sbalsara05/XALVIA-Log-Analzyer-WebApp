package config

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"ricovr/gateway/models"
)

// LoadACL parses acl.csv at path and returns a populated ACLStore.
// Expected columns: firebase_uid, email, organization_id, role, read, write
func LoadACL(path string) (*models.ACLStore, error) {
	store := models.NewACLStore()

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	for i, row := range records {
		if i == 0 {
			continue // skip header
		}
		if len(row) < 6 {
			return nil, fmt.Errorf("%s row %d: expected 6 columns, got %d", path, i+1, len(row))
		}
		store.AddUser(models.User{
			FirebaseUID:    strings.TrimSpace(row[0]),
			Email:          strings.TrimSpace(row[1]),
			OrganizationID: strings.TrimSpace(row[2]),
			Role:           strings.TrimSpace(row[3]),
			Read:           strings.TrimSpace(row[4]) == "true",
			Write:          strings.TrimSpace(row[5]) == "true",
		})
	}
	return store, nil
}

// LoadDevices parses devices.csv at path and adds device entries to store.
// Expected columns: mac_id, organization_id
func LoadDevices(store *models.ACLStore, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	for i, row := range records {
		if i == 0 {
			continue // skip header
		}
		if len(row) < 2 {
			return fmt.Errorf("%s row %d: expected 2 columns, got %d", path, i+1, len(row))
		}
		store.AddDevice(models.DeviceEntry{
			MacID:          strings.TrimSpace(row[0]),
			OrganizationID: strings.TrimSpace(row[1]),
		})
	}
	return nil
}
