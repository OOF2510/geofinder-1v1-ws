package main

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/websocket/v2"
	"github.com/sirupsen/logrus"
)

var matchStore *MatchStore
var discoveryConnections []*websocket.Conn
var discoveryMutex sync.RWMutex

func cleanupFinishedMatches() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		matchStore.mutex.RLock()
		for hash, match := range matchStore.matches {
			if match.State == "finished" {
				if time.Since(match.CreatedAt) > 10*time.Minute {
					LogMatchLifecycle(hash, "cleanup", logrus.Fields{
						"match_age_seconds": int64(time.Since(match.CreatedAt).Seconds()),
					})
					matchStore.DeleteMatch(hash)
				}
			}
		}
		matchStore.mutex.RUnlock()
	}
}

func handleGameConnection(c *websocket.Conn, router *EventRouter) {
	hash := c.Query("roomHash")
	if hash == "" {
		c.WriteJSON(map[string]interface{}{"error": "Missing roomHash"})
		return
	}

	hashRes, err := VerifyHash(hash)
	if err != nil || !hashRes.Ok {
		c.WriteJSON(map[string]interface{}{"error": "Invalid room hash"})
		return
	}

	match, _ := matchStore.GetMatch(hash)
	if match == nil {
		match = matchStore.CreateMatch(hash)
	}

	LogWebSocketConnection(hash, "", "", true)

	for {
		var message struct {
			Event string      `json:"event"`
			Data  interface{} `json:"data"`
		}
		err := c.ReadJSON(&message)
		if err != nil {
			LogWebSocketError(hash, err)
			break
		}

		dataMap := message.Data.(map[string]interface{})
		dataMap["hash"] = hash

		router.Handle(c, message.Event, dataMap)
	}

}

func handleDiscoveryConnection(c *websocket.Conn) {
	// Add to discovery connections
	discoveryMutex.Lock()
	discoveryConnections = append(discoveryConnections, c)
	discoveryMutex.Unlock()
	LogDiscoveryConnection(true, len(discoveryConnections))

	// Send initial waiting rooms
	rooms := matchStore.GetWaitingRooms()
	c.WriteJSON(map[string]interface{}{
		"type": "rooms_list",
		"data": rooms,
	})

	// Handle disconnection
	go func() {
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				discoveryMutex.Lock()
				for i, conn := range discoveryConnections {
					if conn == c {
						discoveryConnections = append(discoveryConnections[:i], discoveryConnections[i+1:]...)
						LogDiscoveryConnection(false, len(discoveryConnections))
						break
					}
				}
				discoveryMutex.Unlock()
				break
			}
		}
	}()
}

func roundTimeoutChecker(ctx context.Context, hash string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			match, exists := matchStore.GetMatch(hash)
			if !exists {
				return
			}

			match.mutex.RLock()
			matchState := match.State
			currentRound := match.GameState.CurrentRound
			match.mutex.RUnlock()

			if matchState != "playing" {
				return
			}

			roundNum := currentRound - 1
			if roundNum < 0 || roundNum >= 5 {
				return
			}

			match.mutex.RLock()
			round := match.GameState.Rounds[roundNum]
			match.mutex.RUnlock()

			if IsRoundTimeUp(&round) {
				shouldEnd, _ := matchStore.ShouldEndRound(hash)
				if shouldEnd && !round.Finished {
					result, err := matchStore.EndRound(hash)
					if err != nil {
						LogGameRound(hash, roundNum+1, "timeout_error", logrus.Fields{
							"error": err.Error(),
						})
						return
					}

					matchStore.BroadcastToRoom(hash, result)

					match, _ := matchStore.GetMatch(hash)

					match.mutex.RLock()
					currentRound := match.GameState.CurrentRound
					hostScore := match.GameState.HostScore
					guestScore := match.GameState.GuestScore
					match.mutex.RUnlock()

					if currentRound < 5 {
						time.AfterFunc(3*time.Second, func() {
							matchStore.StartNextRound(hash)
						})
					} else {
						gameEnd := GameEndPayload{
							Type:       "game_end",
							HostScore:  hostScore,
							GuestScore: guestScore,
							Winner:     GetWinner(hostScore, guestScore),
						}
						matchStore.BroadcastToRoom(hash, gameEnd)

						match.mutex.Lock()
						match.State = "finished"
						match.mutex.Unlock()

						PublishRoomState(hash, "finished", 2)
					}
				}
			}
		}
	}
}

func main() {
	matchStore = NewMatchStore()

	if err := InitRedis(); err != nil {
		LogRedisError("init", err)
	}

	go func() {
		ctx := context.Background()
		SubscribeToRoomUpdates(ctx)
	}()

	go cleanupFinishedMatches()

	app := fiber.New()
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,HEAD,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders: "*",
	}))

	eventRouter := NewEventRouter()

	eventRouter.On("ping", func(conn *websocket.Conn, data interface{}) {
		conn.WriteMessage(websocket.TextMessage, []byte("pong"))
	})

	eventRouter.On("auth", func(conn *websocket.Conn, data interface{}) {
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			conn.WriteJSON(map[string]interface{}{"type": "error", "message": "invalid payload"})
			return
		}

		hash, _ := dataMap["hash"].(string)
		if hash == "" {
			conn.WriteJSON(map[string]interface{}{"type": "error", "message": "Missing hash"})
			return
		}

		playerID, _ := dataMap["playerID"].(string)

		match, exists := matchStore.GetMatch(hash)
		if !exists {
			conn.WriteJSON(map[string]interface{}{
				"type":    "error",
				"message": "Match not found",
			})
			return
		}
		var role string

		if playerID == "" {
			if match.HostConn == nil {
				role = "host"
				id, _ := GeneratePlayerID()
				matchStore.SetConnection(hash, id, role, conn)
				playerID = id
			} else if match.GuestConn == nil {
				role = "guest"
				id, _ := GeneratePlayerID()
				matchStore.SetConnection(hash, id, role, conn)
				playerID = id
			} else {
				conn.WriteJSON(map[string]interface{}{
					"type":    "error",
					"message": "Match is full",
				})
				return
			}
		} else {
			existingRole, found := matchStore.CanReconnect(hash, playerID)
			if !found {
				conn.WriteJSON(map[string]interface{}{
					"type":    "error",
					"message": "Cannot reconnect",
				})
				return
			}
			role = existingRole
			matchStore.SetConnection(hash, playerID, role, conn)
		}

		StorePlayerIDs(hash, match.HostID, match.GuestID)

		authok := AuthOkPayload{
			Type:         "auth_ok",
			PlayerId:     playerID,
			Role:         role,
			RoomState:    match.State,
			CurrentRound: match.GameState.CurrentRound,
			HostScore:    match.GameState.HostScore,
			GuestScore:   match.GameState.GuestScore,
		}
		err := conn.WriteJSON(authok)
		if err != nil {
			LogBroadcastError(hash, role, err)
			return
		}

		playerCount := GetPlayerCount(match)
		if playerCount == 2 && match.State == "waiting" {
			LogMatchEvent(hash, "both_players_connected", logrus.Fields{
				"player_count": playerCount,
			})
			select {
			case <-match.ReadyChan:
				match.mutex.Lock()
				if match.State == "waiting" {
					match.State = "playing"
					LogMatchEvent(hash, "game_started", logrus.Fields{
						"player_count": playerCount,
					})
					PublishRoomState(hash, match.State, playerCount)
				} else {
					LogMatchEvent(hash, "game_already_started", logrus.Fields{
						"current_state": match.State,
					})
				}
				match.mutex.Unlock()

				err := matchStore.StartNextRound(hash)
				if err != nil {
					LogGameRound(hash, 1, "start_error", logrus.Fields{
						"error": err.Error(),
					})
				} else {
					go roundTimeoutChecker(context.Background(), hash)
				}
			case <-time.After(30 * time.Second):
				LogMatchEvent(hash, "game_ready_timeout", logrus.Fields{})
				conn.WriteJSON(map[string]interface{}{
					"type":    "error",
					"message": "Game initialization timeout",
				})
				return
			}
		}
	})

	eventRouter.On("reconnect", func(conn *websocket.Conn, data interface{}) {
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			conn.WriteJSON(map[string]interface{}{"type": "error", "message": "invalid payload"})
			return
		}

		hash, _ := dataMap["hash"].(string)
		if hash == "" {
			conn.WriteJSON(map[string]interface{}{"type": "error", "message": "Missing hash"})
			return
		}

		playerID, _ := dataMap["playerId"].(string)
		if playerID == "" {
			playerID, _ = dataMap["playerID"].(string)
		}

		role, canReconnect := matchStore.CanReconnect(hash, playerID)
		if !canReconnect {
			conn.WriteJSON(map[string]interface{}{
				"type":    "error",
				"message": "Cannot reconnect",
			})
			return
		}

		matchStore.ReconnectPlayer(hash, role, conn)

		match, _ := matchStore.GetMatch(hash)
		conn.WriteJSON(ReconnectOkPayload{
			Type:      "reconnect_ok",
			PlayerId:  playerID,
			Role:      role,
			RoomState: match.State,
			GameState: &match.GameState,
		})
	})

	eventRouter.On("submit_answer", func(conn *websocket.Conn, data interface{}) {
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			conn.WriteJSON(map[string]interface{}{"type": "error", "message": "invalid payload"})
			return
		}

		hash, _ := dataMap["hash"].(string)
		if hash == "" {
			conn.WriteJSON(map[string]interface{}{"type": "error", "message": "Missing hash"})
			return
		}

		playerID, _ := dataMap["playerId"].(string)
		if playerID == "" {
			playerID, _ = dataMap["playerID"].(string)
		}

		countryCode, _ := dataMap["countryCode"].(string)
		countryName, _ := dataMap["countryName"].(string)

		if playerID == "" || countryCode == "" {
			conn.WriteJSON(map[string]interface{}{"type": "error", "message": "Missing fields"})
			return
		}

		matchStore.SubmitAnswer(hash, playerID, countryCode, countryName)

		shouldEnd, _ := matchStore.ShouldEndRound(hash)
		if shouldEnd {
			result, err := matchStore.EndRound(hash)
			if err != nil {
				LogGameRound(hash, 0, "end_error", logrus.Fields{
					"error": err.Error(),
				})
				return
			}

			matchStore.BroadcastToRoom(hash, result)

			match, _ := matchStore.GetMatch(hash)

			match.mutex.RLock()
			currentRound := match.GameState.CurrentRound
			hostScore := match.GameState.HostScore
			guestScore := match.GameState.GuestScore
			match.mutex.RUnlock()

			if currentRound < 5 {
				time.AfterFunc(3*time.Second, func() {
					matchStore.StartNextRound(hash)
				})
			} else {
				gameEnd := GameEndPayload{
					Type:       "game_end",
					HostScore:  hostScore,
					GuestScore: guestScore,
					Winner:     GetWinner(hostScore, guestScore),
				}
				matchStore.BroadcastToRoom(hash, gameEnd)

				match.mutex.Lock()
				match.State = "finished"
				match.mutex.Unlock()

				PublishRoomState(hash, "finished", 2)
			}
		}
	})

	// WebSocket middleware for game connections
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	// Game WebSocket connection
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		handleGameConnection(c, eventRouter)
	}))

	// Discovery WebSocket connection
	app.Use("/ws/discovery", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws/discovery", websocket.New(func(c *websocket.Conn) {
		handleDiscoveryConnection(c)
	}))

	// Status endpoint
	app.Get("/status", func(c *fiber.Ctx) error {
		localTime := time.Now().Local()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		return c.JSON(fiber.Map{
			"status":                "ok",
			"time":                  localTime,
			"active_ws_connections": len(matchStore.matches),
			"memory_stats": map[string]uint64{
				"Alloc":      mem.Alloc,
				"TotalAlloc": mem.TotalAlloc,
				"Sys":        mem.Sys,
				"NumGC":      uint64(mem.NumGC),
			},
		})
	})

	err := app.Listen(":8080")
	if err != nil {
		LogWithFields(logrus.Fields{
			"event": "server_start_error",
			"error": err.Error(),
		}).Fatal("Failed to start server")
	}

	LogWithFields(logrus.Fields{
		"event":   "server_start",
		"address": ":8080",
	}).Info("Server started successfully")
}
