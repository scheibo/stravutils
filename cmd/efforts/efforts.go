package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/scheibo/perf"
	. "github.com/scheibo/stravutils"
	"github.com/strava/go.strava"
)

type Effort struct {
	climb  Climb
	effort *strava.SegmentEffortSummary
	score  float64
}

func main() {
	var token, climbsFile string
	var climbs []Climb
	var efforts []*Effort

	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")

	flag.Parse()

	climbs, err := GetClimbs(climbsFile)
	if err != nil {
		exit(err)
	}

	for _, climb := range climbs {
		es, err := GetEfforts(climb.Segment.ID)
		if err != nil {
			exit(err)
		}

		for _, e := range es {
			score := perf.CalcM(float64(e.ElapsedTime), climb.Segment.Distance, climb.Segment.AverageGrade()/100, climb.Segment.MedianElevation())
			effort := &Effort{climb: climb, effort: e, score: score}
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

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
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
