package main

import (
	"os"

	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func init() {
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetLevel(logrus.InfoLevel)
}

func LogWithFields(fields logrus.Fields) *logrus.Entry {
	return log.WithFields(fields)
}

func LogWebSocketConnection(hash string, role string, playerID string, connected bool) {
	fields := logrus.Fields{
		"event":     "websocket_connection",
		"room_hash": hash,
		"role":      role,
		"player_id": playerID,
		"connected": connected,
	}
	if connected {
		LogWithFields(fields).Info("WebSocket connection established")
	} else {
		LogWithFields(fields).Info("WebSocket connection closed")
	}
}

func LogWebSocketError(hash string, err error) {
	LogWithFields(logrus.Fields{
		"event":     "websocket_error",
		"room_hash": hash,
		"error":     err.Error(),
	}).Error("WebSocket error occurred")
}

func LogRedisPublish(channel string, payload interface{}) {
	LogWithFields(logrus.Fields{
		"event":   "redis_publish",
		"channel": channel,
		"payload": payload,
	}).Debug("Redis publish event")
}

func LogRedisSubscribe(channel string) {
	LogWithFields(logrus.Fields{
		"event":   "redis_subscribe",
		"channel": channel,
	}).Info("Subscribed to Redis channel")
}

func LogRedisMessage(channel string, payload string) {
	LogWithFields(logrus.Fields{
		"event":   "redis_message",
		"channel": channel,
		"payload": payload,
	}).Debug("Received Redis message")
}

func LogRedisError(operation string, err error) {
	LogWithFields(logrus.Fields{
		"event":     "redis_error",
		"operation": operation,
		"error":     err.Error(),
	}).Error("Redis operation failed")
}

func LogMatchEvent(hash string, eventType string, details logrus.Fields) {
	baseFields := logrus.Fields{
		"event":     "match_event",
		"room_hash": hash,
		"type":      eventType,
	}
	for k, v := range details {
		baseFields[k] = v
	}
	LogWithFields(baseFields).Info("Match event")
}

func LogGameRound(hash string, roundNumber int, roundEvent string, details logrus.Fields) {
	baseFields := logrus.Fields{
		"event":        "game_round",
		"room_hash":    hash,
		"round_number": roundNumber,
		"round_event":  roundEvent,
	}
	for k, v := range details {
		baseFields[k] = v
	}
	LogWithFields(baseFields).Info("Game round event")
}

func LogPlayerAction(hash string, playerID string, action string, details logrus.Fields) {
	baseFields := logrus.Fields{
		"event":     "player_action",
		"room_hash": hash,
		"player_id": playerID,
		"action":    action,
	}
	for k, v := range details {
		baseFields[k] = v
	}
	LogWithFields(baseFields).Info("Player action")
}

func LogBroadcastEvent(hash string, messageType string, recipientCount int) {
	LogWithFields(logrus.Fields{
		"event":           "broadcast",
		"room_hash":       hash,
		"message_type":    messageType,
		"recipient_count": recipientCount,
	}).Info("Broadcast to room")
}

func LogBroadcastError(hash string, role string, err error) {
	LogWithFields(logrus.Fields{
		"event":     "broadcast_error",
		"room_hash": hash,
		"role":      role,
		"error":     err.Error(),
	}).Error("Failed to broadcast to connection")
}

func LogDiscoveryConnection(connected bool, totalConnections int) {
	fields := logrus.Fields{
		"event":             "discovery_connection",
		"connected":         connected,
		"total_connections": totalConnections,
	}
	if connected {
		LogWithFields(fields).Info("Discovery connection established")
	} else {
		LogWithFields(fields).Info("Discovery connection closed")
	}
}

func LogMatchLifecycle(hash string, lifecycleEvent string, details logrus.Fields) {
	baseFields := logrus.Fields{
		"event":           "match_lifecycle",
		"room_hash":       hash,
		"lifecycle_event": lifecycleEvent,
	}
	for k, v := range details {
		baseFields[k] = v
	}
	LogWithFields(baseFields).Info("Match lifecycle event")
}
