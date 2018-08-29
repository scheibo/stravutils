package stravutils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/strava/go.strava"
)

const MAX_PER_PAGE = 200

type Climb struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Segment Segment  `json:"segment"`
}

type Segment struct {
	Name          string  `json:"name"`
	ID            int64   `json:"id"`
	Distance      float64 `json:"distance"`
	ElevationLow  float64 `json:"elevation_low"`
	ElevationHigh float64 `json:"elevation_high"`
}

func (s *Segment) TotalElevationGain() float64 {
	return s.ElevationHigh - s.ElevationLow
}

func (s *Segment) MedianElevation() float64 {
	return (s.ElevationLow + s.ElevationHigh) / 2.0
}

func (s *Segment) AverageGrade() float64 {
	return s.TotalElevationGain() / s.Distance * 100.0
}

func GetClimbs(files ...string) ([]Climb, error) {
	var climbs []Climb

	file := resource("data/climbs.json")
	if len(files) > 0 && files[0] != "" {
		file = files[0]
	}

	f, err := ioutil.ReadFile(file)
	if err != nil {
		return climbs, err
	}

	err = json.Unmarshal(f, &climbs)
	if err != nil {
		return climbs, err
	}

	return climbs, nil
}

func GetSegmentByID(segmentID int64, climbs []Climb, tokens ...string) (*Segment, error) {
	for _, c := range climbs {
		if c.Segment.ID == segmentID {
			return &c.Segment, nil
		}
	}

	service, err := getSegmentsService(tokens...)
	if err != nil {
		return nil, err
	}
	s, err := service.Get(segmentID).Do()
	if err != nil {
		return nil, err
	}

	return &Segment{
		ID:            segmentID,
		Name:          s.Name,
		Distance:      s.Distance,
		ElevationHigh: s.ElevationHigh,
		ElevationLow:  s.ElevationLow,
	}, nil
}

func GetEfforts(segmentID int64, maxPages int, tokens ...string) ([]*strava.SegmentEffortSummary, error) {
	var efforts []*strava.SegmentEffortSummary

	service, err := getSegmentsService(tokens...)
	if err != nil {
		return nil, err
	}

	for page := 1; maxPages < 1 || page <= maxPages; page++ {
		es, err := service.ListEfforts(segmentID).
			PerPage(MAX_PER_PAGE).
			Do()

		if err != nil {
			return nil, err
		}

		if len(es) == 0 {
			break
		}

		efforts = append(efforts, es...)
	}

	return efforts, nil
}

func resource(filename string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), filename)
}

func getSegmentsService(tokens ...string) (*strava.SegmentsService, error) {
	token := os.Getenv("STRAVA_ACCESS_TOKEN")
	if len(tokens) > 0 && tokens[0] != "" {
		token = tokens[0]
	}
	if token == "" {
		return nil, fmt.Errorf("must provide a Strava access token")
	}

	client := strava.NewClient(token)
	return strava.NewSegmentsService(client), nil
}
