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
	var starred bool
	var token, climbsFile string
	var climbs, empty, result []Climb
	var err error

	flag.BoolVar(&starred, "starred", false, "Fetch and include starred segments")
	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")

	flag.Parse()

	if climbsFile != "" {
		climbs, err = GetClimbs(climbsFile)
		if err != nil {
			exit(err)
		}
	}

	climbById := make(map[int64]Climb)
	for _, c := range climbs {
		s, err := GetSegmentByID(c.Segment.ID, empty, token)
		if err != nil {
			exit(err)
		}

		nc := Climb{
			Name:    c.Name,
			Aliases: c.Aliases,
			Segment: *s,
		}

		climbById[s.ID] = nc
		result = append(result, nc)
	}

	if starred {
		stars, err := GetStarred(token)
		if err != nil {
			exit(err)
		}

		for _, s := range stars {
			c, ok := climbById[s.Id]
			if !ok {
				// Obnoxiously, we need the SegmentDetailed for TotalElevatioGain
				ns, err := GetSegmentByID(s.Id, empty, token)
				if err != nil {
					exit(err)
				}
				c = Climb{
					Name:    ns.Name,
					Segment: *ns,
				}
				result = append(result, c)
			}
		}
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
