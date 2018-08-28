// > go run brad.go -token=$BRAD_STRAVA_ACCESS_TOKEN -climbs=climbs.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"time"

	"github.com/scheibo/perf"
	"github.com/strava/go.strava"
)

const NUM_BEST = 5
const WEIGHT_BEST = 1
const NUM_YEAR = 5
const WEIGHT_YEAR = 2
const NUM_RECENT = 5
const WEIGHT_RECENT = 4

const MAX_PER_PAGE = 200

type Climb struct {
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases"`
	SegmentId int64    `json:"segment_id"`
}

type Effort struct {
	climb   Climb
	segment *strava.SegmentDetailed
	effort  *strava.SegmentEffortSummary
	score   float64
}

func main() {
	var accessToken string
	var climbsFile string
	var climbs []Climb

	var segments []*strava.SegmentDetailed
	var efforts []*Effort

	// Provide an access token, with write permissions.
	// You'll need to complete the oauth flow to get one.
	flag.StringVar(&accessToken, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")

	flag.Parse()

	if accessToken == "" {
		fmt.Println("\nPlease provide an access_token, one can be found at https://www.strava.com/settings/api")

		flag.PrintDefaults()
		os.Exit(1)
	}

	if climbsFile == "" {
		fmt.Println("\nPlease provide a JSON file name pointing towards climbs.")

		flag.PrintDefaults()
		os.Exit(1)
	}

	file, err := ioutil.ReadFile(climbsFile)
	maybeExit(err)
	err = json.Unmarshal(file, &climbs)
	maybeExit(err)

	client := strava.NewClient(accessToken)
	service := strava.NewSegmentsService(client)
	for _, climb := range climbs {
		segment, err := service.Get(climb.SegmentId).Do()
		maybeExit(err)
		segments = append(segments, segment)

		es, err := service.ListEfforts(climb.SegmentId).
			PerPage(MAX_PER_PAGE).
			Do()
		maybeExit(err)

		h := (segment.ElevationLow + segment.ElevationHigh) / 2
		for _, e := range es {
			score := perf.CalcM(float64(e.ElapsedTime), segment.Distance, averageGrade(segment), h)
			effort := &Effort{climb : climb, segment: segment, effort: e, score: score}
			efforts = append(efforts, effort)
		}
	}

	efforts = sortEfforts(efforts)
	for i, effort := range efforts {
		fmt.Printf("%d) %s: %s = %.2f (%s)\n", i+1, effort.climb.Name,
			(time.Duration(effort.effort.ElapsedTime) * time.Second),
			effort.score, effort.effort.StartDateLocal.Format("Mon Jan _2 3:04PM 2006"))
	}
}

func averageGrade(s *strava.SegmentDetailed) float64 {
	return (s.ElevationHigh - s.ElevationLow) / s.Distance
}

func maybeExit(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type Efforts []*Effort

func sortEfforts(e Efforts) Efforts {
	sort.Sort(sort.Reverse(e))
	return e
}

func (e Efforts) Len() int {
	return len(e)
}

func (e Efforts) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

func (e Efforts) Less(i, j int) bool {
	return e[i].score < e[j].score
}
