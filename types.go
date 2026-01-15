package main

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/websocket/v2"
)

type Coordinates struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type ImageResponse struct {
	ImageURL    string      `json:"imageUrl"`
	Coordinates Coordinates `json:"coordinates"`
	CountryName string      `json:"countryName"`
	CountryCode string      `json:"countryCode"`
	Contributor string      `json:"contributor"`
}

type VerifyHashResponse struct {
	Ok bool `json:"ok"`
}

type EventHandler func(conn *websocket.Conn, data interface{})

type EventRouter struct {
	handlers map[string]EventHandler
}

type AuthPayload struct {
	Type     string `json:"type"`
	Hash     string `json:"hash"`
	ClientID string `json:"clientId,omitempty"`
}

type AuthOkPayload struct {
	Type         string `json:"type"`
	PlayerId     string `json:"playerId"`
	Role         string `json:"role"` // "host" | "guest"
	RoomState    string `json:"roomState"`
	CurrentRound int    `json:"currentRound,omitempty"`
	HostScore    int    `json:"hostScore,omitempty"`
	GuestScore   int    `json:"guestScore,omitempty"`
}

type Match struct {
	Hash      string
	HostConn  *websocket.Conn
	HostID    string
	GuestConn *websocket.Conn
	GuestID   string

	GameState GameState

	State     string // "waiting","ready","playing","finished"
	Seed      int64
	CreatedAt time.Time
	GameReady bool
	ReadyChan chan struct{}
	ReadyOnce sync.Once
	Cancel    context.CancelFunc

	mutex sync.RWMutex // protects fields inside this Match
}

type MatchStore struct {
	mutex   sync.RWMutex
	matches map[string]*Match
}

type PlayerAnswer struct {
	CountryCode string    `json:"countryCode"`
	CountryName string    `json:"countryName"`
	Correct     bool      `json:"correct"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type Round struct {
	ImageURL    string        `json:"imageUrl"`
	CountryCode string        `json:"countryCode"`
	CountryName string        `json:"countryName"`
	Coordinates Coordinates   `json:"coordinates"`
	HostGuess   *PlayerAnswer `json:"hostGuess,omitempty"`
	GuestGuess  *PlayerAnswer `json:"guestGuess,omitempty"`
	StartedAt   time.Time     `json:"startedAt"`
	EndTime     time.Time     `json:"endTime"`
	Finished    bool          `json:"finished"`
}

type GameState struct {
	CurrentRound int      `json:"currentRound"`
	Rounds       [5]Round `json:"rounds"`
	HostScore    int      `json:"hostScore"`
	GuestScore   int      `json:"guestScore"`
}

type RoundStartPayload struct {
	Type       string    `json:"type"`
	RoundIndex int       `json:"roundIndex"`
	ImageURL   string    `json:"imageUrl"`
	EndTime    time.Time `json:"endTime"`
}

type AnswerPayload struct {
	Type        string `json:"type"`
	PlayerID    string `json:"playerId"`
	CountryCode string `json:"countryCode"`
	CountryName string `json:"countryName"`
}

type RoundResultPayload struct {
	Type        string  `json:"type"`
	RoundIndex  int     `json:"roundIndex"`
	HostAnswer  *string `json:"hostAnswer"`
	GuestAnswer *string `json:"guestAnswer"`
	CorrectCode string  `json:"correctCode"`
	CorrectName string  `json:"correctName"`
	HostScore   int     `json:"hostScore"`
	GuestScore  int     `json:"guestScore"`
}

type GameEndPayload struct {
	Type       string `json:"type"`
	HostScore  int    `json:"hostScore"`
	GuestScore int    `json:"guestScore"`
	Winner     string `json:"winner"` // "host", "guest", "tie"
}

type RoomStatePayload struct {
	Type        string `json:"type"`
	Hash        string `json:"hash"`
	State       string `json:"state"` // "waiting","ready","playing","finished"
	PlayerCount int    `json:"playerCount"`
}

type ReconnectPayload struct {
	Type     string `json:"type"`
	Hash     string `json:"hash"`
	PlayerID string `json:"playerId"`
}

type ReconnectOkPayload struct {
	Type      string     `json:"type"`
	PlayerId  string     `json:"playerId"`
	Role      string     `json:"role"` // "host" | "guest"
	RoomState string     `json:"roomState"`
	GameState *GameState `json:"gameState,omitempty"`
}
