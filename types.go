package main

import "github.com/gorilla/websocket"

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

type VerifyHashResponse struct {
	Ok bool `json:"ok"`
}

type EventHandler func(conn *websocket.Conn, data interface{})

type EventRouter struct {
 handlers map[string]EventHandler
}