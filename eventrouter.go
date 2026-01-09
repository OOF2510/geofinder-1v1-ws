package main

import "github.com/gorilla/websocket"

// registers an event and its handler
func (router *EventRouter) On(event string, handler EventHandler) {
 router.handlers[event] = handler
}

// routes incoming event to the appropriate handler
func (router *EventRouter) Handle(conn *websocket.Conn, event string, data interface{}) {
 if handler, ok := router.handlers[event]; ok {
  handler(conn, data)
 } else {
  // Handle unknown events
  conn.WriteMessage(websocket.TextMessage, []byte("Unknown event: "+event))
 }
}

func NewEventRouter() *EventRouter {
 return &EventRouter{handlers: make(map[string]EventHandler)}
}