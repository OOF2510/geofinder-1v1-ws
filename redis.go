package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/gofiber/websocket/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
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
		LogRedisError("connect", err)
		return err
	}

	LogWithFields(logrus.Fields{
		"event": "redis_connected",
	}).Info("Successfully connected to Redis")
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
	publishErr := redisClient.Publish(ctx, "geofinder:room_updates", data).Err()
	if publishErr != nil {
		LogRedisError("publish", publishErr)
	} else {
		LogRedisPublish("geofinder:room_updates", payload)
	}
	return publishErr
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

func SubscribeToRoomUpdates(ctx context.Context) {
	pubsub := redisClient.Subscribe(ctx, "geofinder:room_updates")
	defer pubsub.Close()

	LogRedisSubscribe("geofinder:room_updates")

	ch := pubsub.Channel()
	for msg := range ch {
		LogRedisMessage("geofinder:room_updates", msg.Payload)

		// Broadcast to all discovery connections
		discoveryMutex.RLock()
		connections := make([]*websocket.Conn, len(discoveryConnections))
		copy(connections, discoveryConnections)
		discoveryMutex.RUnlock()

		for _, conn := range connections {
			err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
			if err != nil {
				LogBroadcastError("", "discovery_client", err)
			}
		}
	}
}
