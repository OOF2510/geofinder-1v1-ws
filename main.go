package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

	server.GET("/ws", func(ctx *gin.Context) {
		// check for hash query param
		hash := ctx.Query("roomHash")
		if hash == "" {
			ctx.JSON(401, gin.H{"error": "Missing roomHash parameter"})
			return
		}

		hashRes, err := VerifyHash(hash)
		if err != nil {
			ctx.JSON(401, gin.H{"error": "Error verifying room hash"})
			return
		}
		if !hashRes.Ok {
			ctx.JSON(401, gin.H{"error": "Invalid room hash"})
			return
		}

		handleWebSocket(ctx, eventRouter)
	})

	err := server.Run(":8080")
	if err != nil {
		fmt.Println("Failed to start server:", err)
		panic("Failed to start server")
	}

	log.Println("Server started on http://localhost:8080")
}
