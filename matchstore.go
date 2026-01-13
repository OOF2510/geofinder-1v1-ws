package main

import (
	"fmt"
	"log"
	"time"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
		GameState: GameState{
			Rounds: [5]Round{},
		},
	}
	store.matches[hash] = match
	log.Printf("Created new match: %s", hash)
	return match
}

func (store *MatchStore) DeleteMatch(hash string) {
	store.mutex.Lock()
	delete(store.matches, hash)
	store.mutex.Unlock()
	log.Printf("Deleted match: %s", hash)
}

func GeneratePlayerID() (string, error) {
	id, err := uuid.NewUUID()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func (store *MatchStore) SetConnection(hash string, playerID string, role string, conn *websocket.Conn) error {
	match, exists := store.GetMatch(hash)
	if !exists {
		return fmt.Errorf("Match %s not found", hash)
	}

	match.mutex.Lock()
	defer match.mutex.Unlock()

	switch role {
		case "host":
			match.HostConn = conn
			match.HostID = playerID
			log.Printf("Set host connection for match %s, player %s", hash, playerID)
		case "guest":
			match.GuestConn = conn
			match.GuestID = playerID
			log.Printf("Set guest connection for match %s, player %s", hash, playerID)
		default:
			return fmt.Errorf("Invalid role: %s", role)
	}

	return nil
}

func (store *MatchStore) RemoveConnection(hash string, role string) error {
	match, exists := store.GetMatch(hash)
	if !exists {
		return fmt.Errorf("Match %s not found", hash)
	}

	match.mutex.Lock()
	defer match.mutex.Unlock()

	switch role {
	case "host":
		match.HostConn = nil
		log.Printf("Removed host connection for match %s", hash)
	case "guest":
		match.GuestConn = nil
		log.Printf("Removed guest connection for match %s", hash)
	default:
		return fmt.Errorf("Invalid role: %s", role)
	}

	return nil
}

func (store *MatchStore) BroadcastToRoom(hash string, message interface{}) error {
	match, exists := store.GetMatch(hash)
	if !exists {
		return fmt.Errorf("Match %s not found", hash)
	}

	match.mutex.RLock()
	defer match.mutex.RUnlock()

	msg, err := json.Marshal(message)
	if err != nil {
		return err
	}

	if match.HostConn != nil {
		if err := match.HostConn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("Error sending message to host in match %s: %v", hash, err)
		}
	}
	if match.GuestConn != nil {
		if err := match.GuestConn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("Error sending message to guest in match %s: %v", hash, err)
		}
	}

	return nil
}

func (store *MatchStore) GetGameState(hash string) (*GameState, bool) {
	match, exists := store.GetMatch(hash)
	if !exists {
		return nil, false
	}

	match.mutex.RLock()
	defer match.mutex.RUnlock()

	return &match.GameState, true
}

