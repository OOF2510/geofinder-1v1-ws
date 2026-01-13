package main

import (
	"github.com/gorilla/websocket"
	"sync"
	"time"
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
    Type      string `json:"type"`
    PlayerId  string `json:"playerId"`
    Role      string `json:"role"` // "host" | "guest"
    RoomState string `json:"roomState"`
}

type Match struct {
	Hash      string
	HostConn  *websocket.Conn
	HostID    string
	GuestConn *websocket.Conn
	GuestID   string

	State     string // "waiting","ready","playing","finished"
	Seed      int64
	CreatedAt time.Time

	mutex sync.Mutex // protects fields inside this Match
}

type MatchStore struct {
	mutex   sync.RWMutex
	matches map[string]*Match
}
