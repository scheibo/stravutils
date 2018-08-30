package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	. "github.com/scheibo/stravutils"
	"github.com/strava/go.strava"
)

func main() {
	var token, climbsFile string

	var climbs []Climb
	var result []Climb

	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")

	flag.Parse()

	climbs, err := GetClimbs(climbsFile)
	if err != nil {
		exit(err)
	}

	climbById := make(map[int64]Climb)
	for _, c := range climbs {
		climbById[c.Segment.ID] = c
	}

	starred, err := GetStarred(token)
	if err != nil {
		exit(err)
	}

	for _, s := range starred {
		c, ok := climbById[s.Id]
		if !ok {
			c = Climb{
				Name: s.Name,
				Segment: Segment{
					ID:            s.Id,
					Name:          s.Name,
					Distance:      s.Distance,
					ElevationHigh: s.ElevationHigh,
					ElevationLow:  s.ElevationLow,
				},
			}
		}
		result = append(result, c)
	}

	j, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		exit(err)
	}
	fmt.Println(string(j))
}

func GetStarred(tokens ...string) ([]*strava.PersonalSegmentSummary, error) {
	var segments []*strava.PersonalSegmentSummary

	service, err := GetCurrentAthleteService(tokens...)
	if err != nil {
		return nil, err
	}

	for page := 1; ; page++ {
		s, err := service.ListStarredSegments().
			PerPage(MAX_PER_PAGE).
			Page(page).
			Do()

		if err != nil {
			return nil, err
		}

		if len(s) == 0 {
			break
		}

		segments = append(segments, s...)
	}

	return segments, nil
}

func GetCurrentAthleteService(tokens ...string) (*strava.CurrentAthleteService, error) {
	token := os.Getenv("STRAVA_ACCESS_TOKEN")
	if len(tokens) > 0 && tokens[0] != "" {
		token = tokens[0]
	}
	if token == "" {
		return nil, fmt.Errorf("must provide a Strava access token")
	}

	client := strava.NewClient(token)
	return strava.NewCurrentAthleteService(client), nil
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
