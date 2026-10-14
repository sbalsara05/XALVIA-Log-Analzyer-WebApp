package models

import "sync"

// User represents a row in acl.csv.
type User struct {
	FirebaseUID    string
	Email          string
	OrganizationID string
	Role           string // "admin" or "user"
	Read           bool
	Write          bool
}

// DeviceEntry represents a row in devices.csv.
type DeviceEntry struct {
	MacID          string
	OrganizationID string
}

// ACLStore is a thread-safe in-memory store of users and devices loaded from CSV.
type ACLStore struct {
	users   map[string]User        // firebase_uid → User
	devices map[string]DeviceEntry // mac_id → DeviceEntry
	mu      sync.RWMutex
}

func NewACLStore() *ACLStore {
	return &ACLStore{
		users:   make(map[string]User),
		devices: make(map[string]DeviceEntry),
	}
}

func (s *ACLStore) AddUser(u User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.FirebaseUID] = u
}

func (s *ACLStore) AddDevice(d DeviceEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[d.MacID] = d
}

func (s *ACLStore) GetUser(uid string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[uid]
	return u, ok
}

func (s *ACLStore) GetDevice(macID string) (DeviceEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[macID]
	return d, ok
}

func (s *ACLStore) UserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}

func (s *ACLStore) DeviceCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.devices)
}
