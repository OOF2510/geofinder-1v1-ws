package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

var matchStore *MatchStore
var discoveryConnections []*websocket.Conn

func handleWebSocket(ctx *gin.Context, router *EventRouter) {
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)

	if err != nil {
		fmt.Println("Failed to set websocket upgrade:", err)
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Read error:", err)
			break
		}
		var message struct {
			Event string      `json:"event"`
			Data  interface{} `json:"data"`
		}
		err = json.Unmarshal(msg, &message)
		if err != nil {
			fmt.Println("JSON error:", err)
			continue
		}

		router.Handle(conn, message.Event, message.Data)
	}

}

func cleanupFinishedMatches() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		matchStore.mutex.RLock()
		for hash, match := range matchStore.matches {
			if match.State == "finished" {
				if time.Since(match.CreatedAt) > 10*time.Minute {
					matchStore.DeleteMatch(hash)
					log.Printf("Cleaned up finished match: %s", hash)
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
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("WebSocket connected for match %s", hash)

	for {
		var message struct {
			Event string      `json:"event"`
			Data  interface{} `json:"data"`
		}
		err := conn.ReadJSON(&message)
		if err != nil {
			log.Printf("Read error: %v", err)
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
		log.Printf("Discovery WebSocket upgrade failed: %v", err)
		return
	}

	// Add to discovery connections
	discoveryConnections = append(discoveryConnections, conn)
	log.Println("New discovery connection")

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
		if !exists || match.State != "playing" {
			break
		}

		roundNum := match.GameState.CurrentRound - 1
		if roundNum < 0 || roundNum >= 5 {
			break
		}

		round := &match.GameState.Rounds[roundNum]
		if IsRoundTimeUp(round) {
			shouldEnd, _ := matchStore.ShouldEndRound(hash)
			if shouldEnd && !round.Finished {
				result, _ := matchStore.EndRound(hash)
				matchStore.BroadcastToRoom(hash, result)

				match, _ := matchStore.GetMatch(hash)
				if match.GameState.CurrentRound < 5 {
					time.AfterFunc(3*time.Second, func() {
						matchStore.StartNextRound(hash)
					})
				} else {
					gameEnd := GameEndPayload{
						Type:       "game_end",
						HostScore:  match.GameState.HostScore,
						GuestScore: match.GameState.GuestScore,
						Winner:     GetWinner(match.GameState.HostScore, match.GameState.GuestScore),
					}
					matchStore.BroadcastToRoom(hash, gameEnd)
					match.State = "finished"
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
		log.Println("failed to connect to redis" + err.Error())
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

	eventRouter.On("newRound", func(conn *websocket.Conn, data interface{}) {
		imageResp, err := GetImage()
		if err != nil {
			fmt.Println("Error fetching image:", err)
			return
		}
		respData, err := json.Marshal(imageResp)
		if err != nil {
			fmt.Println("Error marshaling image response:", err)
			return
		}
		conn.WriteMessage(websocket.TextMessage, []byte(respData))
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


		go PrefetchRounds(match)

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

		playerCount := GetPlayerCount(match)
		match.mutex.Lock()
		if playerCount == 2 && match.State == "waiting" {
			match.State = "playing"
			PublishRoomState(hash, match.State, playerCount)
			matchStore.StartNextRound(hash)
			go roundTimeoutChecker(hash)
		}
		match.mutex.Unlock()

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
		conn.WriteJSON(authok)
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
			result, _ := matchStore.EndRound(hash)
			matchStore.BroadcastToRoom(hash, result)

			match, _ := matchStore.GetMatch(hash)
			if match.GameState.CurrentRound < 5 {
				time.AfterFunc(3*time.Second, func() {
					matchStore.StartNextRound(hash)
				})
			} else {
				gameEnd := GameEndPayload{
					Type:       "game_end",
					HostScore:  match.GameState.HostScore,
					GuestScore: match.GameState.GuestScore,
					Winner:     GetWinner(match.GameState.HostScore, match.GameState.GuestScore),
				}
				matchStore.BroadcastToRoom(hash, gameEnd)
				match.State = "finished"
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
		fmt.Println("Failed to start server:", err)
		panic("Failed to start server")
	}

	log.Println("Server started on http://localhost:8080")
}
