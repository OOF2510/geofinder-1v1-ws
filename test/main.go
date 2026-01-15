package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
)

type Country struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

var countries = []Country{
	{Code: "US", Name: "United States"},
	{Code: "GB", Name: "United Kingdom"},
	{Code: "FR", Name: "France"},
	{Code: "DE", Name: "Germany"},
	{Code: "JP", Name: "Japan"},
	{Code: "CN", Name: "China"},
	{Code: "BR", Name: "Brazil"},
	{Code: "AU", Name: "Australia"},
	{Code: "CA", Name: "Canada"},
	{Code: "IT", Name: "Italy"},
	{Code: "ES", Name: "Spain"},
	{Code: "MX", Name: "Mexico"},
	{Code: "IN", Name: "India"},
	{Code: "KR", Name: "South Korea"},
	{Code: "RU", Name: "Russia"},
	{Code: "ZA", Name: "South Africa"},
	{Code: "AR", Name: "Argentina"},
	{Code: "EG", Name: "Egypt"},
	{Code: "NG", Name: "Nigeria"},
	{Code: "SE", Name: "Sweden"},
}

type WebSocketClient struct {
	conn            *websocket.Conn
	role            string
	playerId        string
	isAuthenticated bool
	messages        []interface{}
	mu              sync.Mutex
}

func randomCountry() Country {
	return countries[rand.Intn(len(countries))]
}

func createRoom() (string, error) {
	apiURL := "https://geo.api.oof2510.space/1v1/new"
	bypassAppCheck := os.Getenv("BYPASS_APP_CHECK")
	if bypassAppCheck == "" {
		bypassAppCheck = "NEED THIS"
	}

	fmt.Printf("Creating new room via %s...\n", apiURL)
	resp, err := http.Get(fmt.Sprintf("%s?bypassAppCheck=%s", apiURL, bypassAppCheck))
	if err != nil {
		return "", fmt.Errorf("failed to create room: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Hash string `json:"hash"`
		Ok   bool   `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}
	if !result.Ok {
		return "", fmt.Errorf("failed to create room: server returned ok=false")
	}
	fmt.Printf("Created room with hash: %s\n", result.Hash)
	return result.Hash, nil
}

func newClient(urlStr, role string) *WebSocketClient {
	fmt.Printf("[%s] Connecting to %s...\n", role, urlStr)
	conn, _, err := websocket.DefaultDialer.Dial(urlStr, nil)
	if err != nil {
		log.Fatalf("[%s] Failed to connect: %v", role, err)
	}

	client := &WebSocketClient{
		conn:     conn,
		role:     role,
		messages: []interface{}{},
	}

	go client.readMessages()
	return client
}

func (c *WebSocketClient) readMessages() {
	defer c.conn.Close()
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("[%s] WebSocket error: %v\n", c.role, err)
			}
			break
		}

		var parsed interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			fmt.Printf("[%s] Raw message: %s\n", c.role, string(data))
			continue
		}

		c.mu.Lock()
		c.messages = append(c.messages, parsed)
		c.mu.Unlock()

		jsonData, _ := json.MarshalIndent(parsed, "", "  ")
		fmt.Printf("[%s] Received: %s\n", c.role, string(jsonData))

		if msgMap, ok := parsed.(map[string]interface{}); ok {
			msgType, _ := msgMap["type"].(string)

			if msgType == "auth_ok" && !c.isAuthenticated {
				c.mu.Lock()
				c.isAuthenticated = true
				if pid, ok := msgMap["playerId"].(string); ok {
					c.playerId = pid
				}
				if role, ok := msgMap["role"].(string); ok {
					c.role = role
				}
				c.mu.Unlock()
				fmt.Printf("[%s] Authenticated as %s with ID %s\n", c.role, c.role, c.playerId)
			}

			if msgType == "round_start" {
				go func() {
					delay := time.Duration(rand.Intn(20000)+5000) * time.Millisecond
					time.Sleep(delay)
					guess := randomCountry()
					fmt.Printf("[%s] Submitting guess: %s (%s)\n", c.role, guess.Name, guess.Code)

					msg := map[string]interface{}{
						"event": "submit_answer",
						"data": map[string]interface{}{
							"hash":        roomHash,
							"playerId":    c.playerId,
							"countryCode": guess.Code,
							"countryName": guess.Name,
						},
					}
					if err := c.conn.WriteJSON(msg); err != nil {
						fmt.Printf("[%s] Failed to send answer: %v\n", c.role, err)
					}
				}()
			}

			if msgType == "game_end" {
				gameEndMu.Lock()
				gameEndReceived[c.role] = true
				winner, _ := msgMap["winner"].(string)
				hostScore, _ := msgMap["hostScore"].(float64)
				guestScore, _ := msgMap["guestScore"].(float64)
				fmt.Printf("[%s] Game ended! Winner: %s\n", c.role, winner)
				fmt.Printf("[%s] Final Score - Host: %.0f, Guest: %.0f\n", c.role, hostScore, guestScore)

				if gameEndReceived["host"] && gameEndReceived["guest"] {
					fmt.Println("\n=== Test completed - game_end received by both ===")
					os.Exit(0)
				}
				gameEndMu.Unlock()
			}
		}
	}
}

func (c *WebSocketClient) sendAuth(hash string) {
	msg := map[string]interface{}{
		"event": "auth",
		"data":  map[string]string{"hash": hash},
	}
	if err := c.conn.WriteJSON(msg); err != nil {
		fmt.Printf("[%s] Failed to send auth: %v\n", c.role, err)
	}
}

var (
	roomHash        string
	gameEndMu       sync.Mutex
	gameEndReceived = map[string]bool{
		"host":  false,
		"guest": false,
	}
)

func main() {
	isLocal := len(os.Args) > 1 && os.Args[1] == "--local"
	isRender := len(os.Args) > 1 && os.Args[1] == "--render"

	baseURL := "wss://geofinder-1v1-ws.onrender.com"
	if isLocal {
		baseURL = "ws://localhost:8080"
	} else if isRender {
		baseURL = "wss://geofinder-1v1-ws.onrender.com"
	}

	fmt.Println("=== GeoFinder 1v1 WebSocket Test ===")
	fmt.Printf("Mode: %s\n", map[bool]string{true: "Local", false: "Render"}[isLocal])
	fmt.Printf("Base URL: %s\n", baseURL)
	fmt.Println("")

	var err error
	roomHash, err = createRoom()
	if err != nil {
		log.Fatalf("Failed to create room: %v", err)
	}
	fmt.Println("")

	hostWsURL := fmt.Sprintf("%s/ws?roomHash=%s", baseURL, roomHash)
	guestWsURL := fmt.Sprintf("%s/ws?roomHash=%s", baseURL, roomHash)

	host := newClient(hostWsURL, "host")
	guest := newClient(guestWsURL, "guest")

	time.Sleep(1 * time.Second)
	fmt.Println("\n=== Sending auth messages ===")
	host.sendAuth(roomHash)
	guest.sendAuth(roomHash)

	time.Sleep(3 * time.Second)
	if !host.isAuthenticated || !guest.isAuthenticated {
		fmt.Println("Waiting for authentication...")
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	timeout := time.After(3 * time.Minute)

	fmt.Println("\n=== Test running, waiting for game_end... ===")

	for {
		select {
		case <-interrupt:
			fmt.Println("\n=== Test interrupted ===")
			os.Exit(1)
		case <-timeout:
			fmt.Println("\n=== Test timeout - exiting ===")
			host.conn.Close()
			guest.conn.Close()
			os.Exit(1)
		}
	}
}
