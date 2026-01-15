package main

import (
	"context"
	"runtime"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

var upgrader = websocket.Upgrader{}

var matchStore *MatchStore
var discoveryConnections []*websocket.Conn


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

func handleGameConnection(ctx *gin.Context, router *EventRouter) {
	hash := ctx.Query("roomHash")
	if hash == "" {
		ctx.JSON(401, gin.H{"error": "Missing roomHash"})
		return
	}

	hashRes, err := VerifyHash(hash)
	if err != nil || !hashRes.Ok {
		ctx.JSON(401, gin.H{"error": "Invalid room hash"})
		return
	}

	match, _ := matchStore.GetMatch(hash)
	if match == nil {
		match = matchStore.CreateMatch(hash)
	}

	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		LogWebSocketError(hash, err)
		return
	}
	defer conn.Close()

	LogWebSocketConnection(hash, "", "", true)

	for {
		var message struct {
			Event string      `json:"event"`
			Data  interface{} `json:"data"`
		}
		err := conn.ReadJSON(&message)
		if err != nil {
			LogWebSocketError(hash, err)
			break
		}

		dataMap := message.Data.(map[string]interface{})
		dataMap["hash"] = hash

		router.Handle(conn, message.Event, dataMap)
	}

}

func handleDiscoveryConnection(ctx *gin.Context) {
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		LogRedisError("discovery_upgrade", err)
		return
	}

	// Add to discovery connections
	discoveryConnections = append(discoveryConnections, conn)
	LogDiscoveryConnection(true, len(discoveryConnections))

	// Send initial waiting rooms
	rooms := matchStore.GetWaitingRooms()
	conn.WriteJSON(map[string]interface{}{
		"type": "rooms_list",
		"data": rooms,
	})

	// Handle disconnection
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				for i, c := range discoveryConnections {
					if c == conn {
						discoveryConnections = append(discoveryConnections[:i], discoveryConnections[i+1:]...)
						LogDiscoveryConnection(false, len(discoveryConnections))
						break
					}
				}
				break
			}
		}
	}()
}

func roundTimeoutChecker(hash string) {
	for {
		match, exists := matchStore.GetMatch(hash)
		if !exists {
			break
		}

		match.mutex.RLock()
		matchState := match.State
		currentRound := match.GameState.CurrentRound
		match.mutex.RUnlock()

		if matchState != "playing" {
			break
		}

		roundNum := currentRound - 1
		if roundNum < 0 || roundNum >= 5 {
			break
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
					break
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

		time.Sleep(1 * time.Second)
	}
}

func main() {
	matchStore = NewMatchStore()

	if err := InitRedis(); err != nil {
		LogRedisError("init", err)
	}

	go func() {
		ctx := context.Background()
		SubscribeToRoomUpdates(ctx, discoveryConnections)
	}()

	go cleanupFinishedMatches()

	server := gin.Default()
	server.Use((cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"*"},
		AllowHeaders:     []string{"*"},
		AllowCredentials: true,
		AllowWebSockets:  true,
	})))
	server.SetTrustedProxies(nil)

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
					go roundTimeoutChecker(hash)
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

	server.GET("/ws", func(ctx *gin.Context) {
		handleGameConnection(ctx, eventRouter)
	})

	server.GET("/ws/discovery", func(ctx *gin.Context) {
		handleDiscoveryConnection(ctx)
	})

	server.GET("/status", func(ctx *gin.Context) {

		localTime := time.Now().Local()
		//get memory usage
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		ctx.JSON(200, gin.H{
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

	err := server.Run(":8080")
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
