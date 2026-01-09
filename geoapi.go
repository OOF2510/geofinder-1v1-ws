package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const API_BASE_URL string = "https://geo.api.oof2510.space/"

func GetImage() (ImageResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
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

func VerifyHash(hash string) (VerifyHashResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Get(API_BASE_URL + "1v1/verify?hash=" + hash)
	if err != nil {
		fmt.Println("Error verifying hash:", err)
		return VerifyHashResponse{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return VerifyHashResponse{}, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	var result VerifyHashResponse
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return VerifyHashResponse{}, err
	}

	return result, nil
}
