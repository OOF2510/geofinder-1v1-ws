package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var redisClient *redis.Client

func InitRedis() error {
	redisAddr := os.Getenv("REDIS_URL")
	redisAddr = strings.TrimPrefix(redisAddr, "redis://")
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0,
	})
	ctx := context.Background()
	err := redisClient.Ping(ctx).Err()
	if err != nil {
		log.Printf("Failed to connect to Redis %v", err)
		return err
	}

	log.Println("Connected to Redis")
	return nil
}

func PublishRoomState(hash string, state string, playerCount int) error {
	payload := RoomStatePayload{
		Type:        "room_state",
		Hash:        hash,
		State:       state,
		PlayerCount: playerCount,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	ctx := context.Background()
	return redisClient.Publish(ctx, "geofinder:room_updates", data).Err()
}

func StorePlayerIDs(hash string, hostID, guestID string) error {
	data := map[string]string{
		"hostID":  hostID,
		"guestID": guestID,
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	ctx := context.Background()
	return redisClient.Set(ctx, "geofinder:match:"+hash+":players", jsonBytes, 10*time.Minute).Err()
}

func GetPlayerIDs(hash string) (string, string, error) {
	ctx := context.Background()
	data, err := redisClient.Get(ctx, "geofinder:match:"+hash+":players").Result()
	if err != nil {
		return "", "", err
	}

	var result struct {
		HostID  string `json:"hostID"`
		GuestID string `json:"guestID"`
	}
	err = json.Unmarshal([]byte(data), &result)
	return result.HostID, result.GuestID, err
}

func SubscribeToRoomUpdates(ctx context.Context, discoveryConnections []*websocket.Conn) {
	pubsub := redisClient.Subscribe(ctx, "geofinder:room_updates")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		log.Printf("Received room update from Redis: %s", msg.Payload)

		// Broadcast to all discovery connections
		for _, conn := range discoveryConnections {
			err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
			if err != nil {
				log.Printf("Failed to send room update to discovery client: %v", err)
			}
		}
	}
}
