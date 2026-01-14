package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var discoveryMutex sync.Mutex

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
		GameReady: false,
		ReadyChan: make(chan struct{}),
		CreatedAt: time.Now(),
		GameState: GameState{
			Rounds: [5]Round{},
		},
	}
	store.matches[hash] = match
	log.Printf("Created new match: %s", hash)

	go func(m *Match) {
		log.Printf("Prefetching rounds for new match %s", hash)
		ok, err := PrefetchRounds(m)
		if ok && err == nil {
			m.mutex.Lock()
			m.GameReady = true
			m.mutex.Unlock()
			m.ReadyOnce.Do(func() {
				close(m.ReadyChan)
			})
			log.Printf("Match %s is ready", hash)
		} else {
			log.Printf("Failed to prefetch rounds for match %s: %v", hash, err)
		}
	}(match)

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

	msg, err := json.Marshal(message)
	if err != nil {
		return err
	}

	match.mutex.RLock()
	defer match.mutex.RUnlock()

	log.Printf("Broadcasting to match %s: host=%v, guest=%v", hash, match.HostConn != nil, match.GuestConn != nil)

	if match.HostConn != nil {
		if err := match.HostConn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("Error sending message to host in match %s: %v", hash, err)
			return fmt.Errorf("failed to send to host: %w", err)
		}
		log.Printf("Successfully sent message to host in match %s", hash)
	}
	if match.GuestConn != nil {
		if err := match.GuestConn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("Error sending message to guest in match %s: %v", hash, err)
			return fmt.Errorf("failed to send to guest: %w", err)
		}
		log.Printf("Successfully sent message to guest in match %s", hash)
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

func (store *MatchStore) SubmitAnswer(hash, playerID, countryCode, countryName string) error {
	match, exists := store.GetMatch(hash)
	if !exists {
		return fmt.Errorf("Match %s not found", hash)
	}

	match.mutex.Lock()
	defer match.mutex.Unlock()

	roundNum := match.GameState.CurrentRound - 1
	if roundNum < 0 || roundNum >= len(match.GameState.Rounds) {
		return fmt.Errorf("Invalid round number %d for match %s", roundNum, hash)
	}

	round := &match.GameState.Rounds[roundNum]
	answer := &PlayerAnswer{
		CountryCode: countryCode,
		CountryName: countryName,
		SubmittedAt: time.Now(),
	}

	switch playerID {
	case match.HostID:
		round.HostGuess = answer
		log.Printf("Host %s submitted answer for match %s round %d", playerID, hash, roundNum+1)
	case match.GuestID:
		round.GuestGuess = answer
		log.Printf("Guest %s submitted answer for match %s round %d", playerID, hash, roundNum+1)
	default:
		return fmt.Errorf("Player %s not part of match %s", playerID, hash)
	}

	log.Printf("Player %s submitted answer for match %s round %d: %s (%s)", playerID, hash, roundNum+1, countryName, countryCode)
	return nil
}

func (store *MatchStore) ShouldEndRound(hash string) (bool, error) {
	match, exists := store.GetMatch(hash)
	if !exists {
		return false, fmt.Errorf("Match %s not found", hash)
	}

	match.mutex.RLock()
	defer match.mutex.RUnlock()

	roundNum := match.GameState.CurrentRound - 1
	if roundNum < 0 || roundNum >= len(match.GameState.Rounds) {
		return false, fmt.Errorf("Invalid round number %d for match %s", roundNum, hash)
	}

	round := &match.GameState.Rounds[roundNum]

	bothAnswered := round.HostGuess != nil && round.GuestGuess != nil
	timeUp := IsRoundTimeUp(round)

	return bothAnswered || timeUp, nil
}

func (store *MatchStore) EndRound(hash string) (*RoundResultPayload, error) {
	match, exists := store.GetMatch(hash)
	if !exists {
		return nil, fmt.Errorf("Match %s not found", hash)
	}

	match.mutex.Lock()
	defer match.mutex.Unlock()

	roundNum := match.GameState.CurrentRound - 1
	if roundNum < 0 || roundNum >= len(match.GameState.Rounds) {
		return nil, fmt.Errorf("Invalid round number %d for match %s", roundNum, hash)
	}

	round := &match.GameState.Rounds[roundNum]
	if round.Finished {
		return nil, fmt.Errorf("Round %d for match %s already finished", roundNum, hash)
	}

	hostCorrect, guestCorrect := GetRoundResult(round)
	if round.HostGuess != nil {
		round.HostGuess.Correct = hostCorrect
	}
	if round.GuestGuess != nil {
		round.GuestGuess.Correct = guestCorrect
	}

	if hostCorrect {
		match.GameState.HostScore++
	}
	if guestCorrect {
		match.GameState.GuestScore++
	}

	round.Finished = true

	var hostAnswer *string
	var guestAnswer *string

	if round.HostGuess != nil {
		ans := fmt.Sprintf("%s (%s)", round.HostGuess.CountryName, round.HostGuess.CountryCode)
		hostAnswer = &ans
	}
	if round.GuestGuess != nil {
		ans := fmt.Sprintf("%s (%s)", round.GuestGuess.CountryName, round.GuestGuess.CountryCode)
		guestAnswer = &ans
	}

	result := &RoundResultPayload{
		Type:        "round_result",
		RoundIndex:  roundNum + 1,
		HostAnswer:  hostAnswer,
		GuestAnswer: guestAnswer,
		CorrectName: round.CountryName,
		CorrectCode: round.CountryCode,
		HostScore:   match.GameState.HostScore,
		GuestScore:  match.GameState.GuestScore,
	}

	log.Printf("Ended round %d for match %s: host correct=%v, guest correct=%v", roundNum+1, hash, hostCorrect, guestCorrect)
	return result, nil
}

func (store *MatchStore) StartNextRound(hash string) error {
	match, exists := store.GetMatch(hash)
	if !exists {
		return fmt.Errorf("Match %s not found", hash)
	}

	match.mutex.Lock()
	defer match.mutex.Unlock()

	roundNum := match.GameState.CurrentRound
	if roundNum >= 5 {
		return fmt.Errorf("All rounds already played for match %s", hash)
	}

	round := &match.GameState.Rounds[roundNum]
	round.StartedAt = time.Now()
	round.EndTime = round.StartedAt.Add(30 * time.Second)
	match.GameState.CurrentRound++

	payload := RoundStartPayload{
		Type:       "round_start",
		RoundIndex: roundNum + 1,
		ImageURL:   round.ImageURL,
		EndTime:    round.EndTime,
	}

	log.Printf("Started round %d for match %s", roundNum+1, hash)
	return store.BroadcastToRoom(hash, payload)
}

func (store *MatchStore) CanReconnect(hash string, playerID string) (string, bool) {
	match, exists := store.GetMatch(hash)
	if !exists {
		return "", false
	}

	match.mutex.RLock()
	defer match.mutex.RUnlock()

	if match.HostID == playerID {
		return "host", true
	}
	if match.GuestID == playerID {
		return "guest", true
	}
	return "", false
}

func (store *MatchStore) ReconnectPlayer(hash string, role string, conn *websocket.Conn) error {
	match, exists := store.GetMatch(hash)
	if !exists {
		return fmt.Errorf("Match %s not found", hash)
	}

	var playerID string
	switch role {
	case "host":
		playerID = match.HostID
	case "guest":
		playerID = match.GuestID
	}

	return store.SetConnection(hash, playerID, role, conn)
}

func (store *MatchStore) GetWaitingRooms() []RoomStatePayload {
	store.mutex.RLock()
	defer store.mutex.RUnlock()

	var rooms []RoomStatePayload
	for _, match := range store.matches {
		if match.State == "waiting" {
			playerCount := GetPlayerCount(match)
			rooms = append(rooms, RoomStatePayload{
				Type:        "room_state",
				Hash:        match.Hash,
				State:       match.State,
				PlayerCount: playerCount,
			})
		}
	}
	return rooms
}
