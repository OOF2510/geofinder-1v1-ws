package main

import (
	"time"

	"github.com/google/uuid"
)

func NewMatchStore() *MatchStore {
	return &MatchStore{
		matches: make(map[string]*Match),
	}
}

func (store *MatchStore) GetMatch(hash string) (*Match, bool) {
	store.mutex.RLock()
	match, exitsts := store.matches[hash]
	store.mutex.RUnlock()
	return match, exitsts
}

func (store *MatchStore) CreateMatch(hash string) *Match {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	if match, exists := store.matches[hash]; exists {
		return match
	}

	match := &Match{
		Hash:      hash,
		State:     "waiting",
		CreatedAt: time.Now(),
	}
	store.matches[hash] = match
	return match
}

func (store *MatchStore) DeleteMatch(hash string) {
	store.mutex.Lock()
	delete(store.matches, hash)
	store.mutex.Unlock()
}

func GeneratePlayerID() (string, error) {
	id, err := uuid.NewUUID()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
