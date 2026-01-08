package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const API_BASE_URL string = "https://geo.api.oof2510.space/"

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


type EventHandler func(conn *websocket.Conn, data interface{})

type EventRouter struct {
 handlers map[string]EventHandler
}

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

var upgrader = websocket.Upgrader{}

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

func GetImage() (ImageResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(API_BASE_URL + "getImage")
	if err != nil {
		return ImageResponse{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return ImageResponse{}, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	var result ImageResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return ImageResponse{}, err
	}

	return result, nil
}

func main() {
	server := gin.Default()
	server.Use((cors.New(cors.Config{
  		AllowOrigins: []string{"*"},
		AllowMethods: []string{"*"},
		AllowHeaders: []string{"*"},
		AllowCredentials: true,
		AllowWebSockets: true,
	})))

	eventRouter := NewEventRouter()

	eventRouter.On("ping", func(conn *websocket.Conn, data interface{}) {
  		conn.WriteMessage(websocket.TextMessage, []byte("pong"))
	})

	server.GET("/ws", func(ctx *gin.Context) {
		handleWebSocket(ctx, eventRouter)
	})

	err := server.Run(":8080")
	if err != nil {
		fmt.Println("Failed to start server:", err)
		panic("Failed to start server")
	}
	log.Println("Server started on http://localhost:8080")

	imageResp, err := GetImage()
	if err != nil {
		fmt.Println("Error fetching image:", err)
		return
	}

	fmt.Printf("Image URL: %s\n", imageResp.ImageURL)
	fmt.Printf("Coordinates: Lat %.6f, Lon %.6f\n", imageResp.Coordinates.Lat, imageResp.Coordinates.Lon)
	fmt.Printf("Country: %s (%s)\n", imageResp.CountryName, imageResp.CountryCode)
	fmt.Printf("Contributor: %s\n", imageResp.Contributor)
}
