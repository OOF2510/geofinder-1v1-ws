package main

import (
	"time"

	"github.com/sirupsen/logrus"
)

func PrefetchRounds(match *Match) (ok bool, err error) {
	LogMatchLifecycle(match.Hash, "prefetch_start", logrus.Fields{})

	for i := range 5 {
		var imgResp ImageResponse

		for j := range 5 {
			imgResp, err = GetImage()
			if err == nil {
				LogGameRound(match.Hash, i+1, "image_fetched", logrus.Fields{
					"image_url": imgResp.ImageURL,
				})
				break
			}
			LogGameRound(match.Hash, i+1, "image_fetch_retry", logrus.Fields{
				"attempt": j + 1,
				"error":   err.Error(),
			})
			time.Sleep(1 * time.Second)
		}

		if err != nil {
			LogGameRound(match.Hash, i+1, "image_fetch_failed", logrus.Fields{
				"error": err.Error(),
			})
			return false, err
		}

		round := Round{
			ImageURL:    imgResp.ImageURL,
			CountryCode: imgResp.CountryCode,
			CountryName: imgResp.CountryName,
			Coordinates: Coordinates{
				Lon: imgResp.Coordinates.Lon,
				Lat: imgResp.Coordinates.Lat,
			},
			Finished: false,
		}

		match.mutex.Lock()
		match.GameState.Rounds[i] = round
		match.mutex.Unlock()
	}
	LogMatchLifecycle(match.Hash, "prefetch_complete", logrus.Fields{})
	return true, nil
}

func GetRoundResult(round *Round) (hostCorrect bool, guestCorrect bool) {
	hostCorrect = round.HostGuess != nil && (round.HostGuess.CountryCode == round.CountryCode || round.HostGuess.CountryName == round.CountryName)
	guestCorrect = round.GuestGuess != nil && (round.GuestGuess.CountryCode == round.CountryCode || round.GuestGuess.CountryName == round.CountryName)
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
	match.mutex.RLock()
	defer match.mutex.RUnlock()

	count := 0
	if match.HostConn != nil {
		count++
	}
	if match.GuestConn != nil {
		count++
	}
	return count
}
