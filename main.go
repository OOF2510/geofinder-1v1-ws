package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
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
