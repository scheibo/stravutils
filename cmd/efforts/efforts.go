package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/scheibo/calc"
	"github.com/scheibo/perf"
	. "github.com/scheibo/stravutils"
	"github.com/strava/go.strava"
)

type Effort struct {
	climb  Climb
	effort *strava.SegmentEffortSummary
	score  float64
	power  float64
}

func main() {
	var best bool
	var token, climbsFile string
	var cda, mt, mr, mb float64

	var climbs []Climb
	var efforts []*Effort

	flag.BoolVar(&best, "best", false, "Best effort per climb only")
	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")

	flag.Float64Var(&cda, "cda", 0.325, "coefficient of drag area")
	flag.Float64Var(&mr, "mr", 67.0, "total mass of the rider in kg")
	flag.Float64Var(&mb, "mb", 8.0, "total mass of the bicycle in kg")

	flag.Parse()

	verify("cda", cda)
	verify("mr", mr)
	verify("mb", mb)
	mt = mr + mb

	climbs, err := GetClimbs(climbsFile)
	if err != nil {
		exit(err)
	}

	maxPages := -1
	if best {
		maxPages = 1
	}

	for _, climb := range climbs {
		es, err := GetEfforts(climb.Segment.ID, maxPages, token)
		if err != nil {
			exit(err)
		}

		for _, e := range es {
			gr := climb.Segment.AverageGrade
			score := perf.CalcM(float64(e.ElapsedTime), climb.Segment.Distance, gr, climb.Segment.MedianElevation)
			power := power(float64(e.ElapsedTime), climb.Segment.Distance, gr, climb.Segment.MedianElevation, mt, cda)

			effort := &Effort{climb: climb, effort: e, score: score, power: power}
			efforts = append(efforts, effort)

			// Only care about the first effort if --best
			if best {
				break
			}
		}
	}

	efforts = sortEfforts(efforts)
	for i, effort := range efforts {
		fmt.Printf("%d) %s: %s = %.2f / %.2fW (%s)\n", i+1, effort.climb.Name,
			(time.Duration(effort.effort.ElapsedTime) * time.Second),
			effort.score, effort.power, effort.effort.StartDateLocal.Format("Mon Jan _2 3:04PM 2006"))
	}
}

func power(t, d, gr, h, mt, cda float64) float64 {
	vg := d / t
	return calc.Psimp(calc.Rho(h, calc.G), cda, calc.Crr, vg, vg, gr, mt, calc.G, calc.Ec, calc.Fw)
}

func verify(s string, x float64) {
	if x < 0 {
		exit(fmt.Errorf("%s must be non negative but was %f", s, x))
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
