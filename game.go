package main

import (
	"log"
	"time"
)


func PrefetchRounds(match *Match) (ok bool, err error) {
	log.Printf("Prefetching rounds for match %s", match.Hash)

	for i := range 5 {
		var imgResp ImageResponse
	    
		for j := range 5 {
			imgResp, err = GetImage()
			if err == nil {
				log.Printf("Fetched round %d image for match %s: %s", i+1, match.Hash, imgResp.ImageURL)
				break
			}
			ok = false
			log.Printf("Error fetching image for match %s: %v. Retrying..., try %d", match.Hash, err, j+1)
			time.Sleep(1 * time.Second)
		}

		match.GameState.Rounds[i] = Round{
			ImageURL:    imgResp.ImageURL,
			CountryCode: imgResp.CountryCode,
			CountryName: imgResp.CountryName,
			Coordinates: Coordinates{
				Lon:  imgResp.Coordinates.Lon,
				Lat: imgResp.Coordinates.Lat,
			},
			Finished:  false,
		}
	}
	log.Printf("Successfully prefetched all rounds for match %s", match.Hash)
	return true, nil
}

func GetRoundResult(round *Round) (hostCorrect bool, guestCorrect bool) {
	hostCorrect = round.HostGuess != nil && round.HostGuess.Correct
	guestCorrect = round.GuestGuess != nil && round.GuestGuess.Correct
	return
}


func IsRoundTimeUp(round *Round) bool {
	if round.StartedAt.IsZero() {
		return false
	}
	elapsed := time.Since(round.StartedAt)
	return elapsed >= 30*time.Second
}

func GetWinner(hostscore, guestscore int) string {
	if hostscore > guestscore {
		return "host"
	} else if guestscore > hostscore {
		return "guest"
	} else {
		return "tie"
	}
}

func GetPlayerCount(match *Match) int {
	count := 0
	if match.HostConn != nil {
		count++
	}
	if match.GuestConn != nil {
		count++
	}
	return count
}