// > go run goals.go -token=$STRAVA_ACCESS_TOKEN -goals=goals.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"

	"os"
	"sort"
	"time"

	"github.com/scheibo/calc"
	"github.com/scheibo/perf"
	. "github.com/scheibo/stravutils"
	"github.com/strava/go.strava"
)

type SegmentGoal struct {
	Name      string `json:"name"`
	SegmentID int64  `json:"segmentId"`
	Goal      int    `json:"goal"`
	Date      int    `json:"date"`
}

type GoalProgress struct {
	SegmentGoal
	BestEffort          int    `json:"bestEffort"`
	BestEffortDuration  string `json:"bestEffortDuration"`
	NumEfforts          int    `json:"numEfforts"`
	BestAttempt         int    `json:"bestAttempt"`
	BestAttemptDuration string `json:"bestAttemptDuration"`
	NumAttempts         int    `json:"numAttempts"`
	GoalDuration        string `json:"goalDuration"`
}

func main() {
	var outputJson bool
	var token, goalsFile, climbsFile string
	var cda, mt, mr, mb float64

	var climbs []Climb

	flag.BoolVar(&outputJson, "json", false, "Output JSON")
	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&goalsFile, "goals", "", "Goals")
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

	file := Resource("goals")
	if goalsFile != "" {
		file = goalsFile
	}

	f, err := ioutil.ReadFile(file)
	if err != nil {
		exit(err)
	}

	var goals []SegmentGoal
	err = json.Unmarshal(f, &goals)
	if err != nil {
		exit(err)
	}

	progress, err := getProgressOnGoals(goals, token)
	if err != nil {
		exit(err)
	}

	if outputJson {
		j, err := json.MarshalIndent(progress, "", "  ")
		if err != nil {
			exit(err)
		}
		fmt.Println(string(j))
	} else {

		for _, goal := range progress {
			segment, err := GetSegmentByID(goal.SegmentID, climbs, token)
			if err != nil {
				exit(err)
			}

			fmt.Printf("%s: %v (%.2f/%.2f) -> %v (%.2f/%.2f) (%.2f%%)",
				goal.Name,
				goal.BestEffortDuration,
				calcPerf(goal.BestEffort, segment),
				calcPower(goal.BestEffort, cda, mt, segment),
				goal.GoalDuration,
				calcPerf(goal.Goal, segment),
				calcPower(goal.Goal, cda, mt, segment),
				float64(goal.Goal)*100.0/float64(goal.BestEffort))
			if goal.NumAttempts > 0 {
				fmt.Printf(" => %v (%.2f/%.2f) (%.2f%%)",
					goal.BestAttemptDuration,
					calcPerf(goal.BestAttempt, segment),
					calcPower(goal.BestAttempt, cda, mt, segment),
					float64(goal.Goal)*100.0/float64(goal.BestAttempt))
			}
			fmt.Printf(" [%v/%v]\n", goal.NumAttempts, goal.NumEfforts)
		}
	}
}

func calcPerf(t int, segment *Segment) float64 {
	return perf.CalcM(float64(t), segment.Distance, segment.AverageGrade()/100, segment.MedianElevation())
}

func calcPower(t int, cda, mt float64, segment *Segment) float64 {
	vg := segment.Distance / float64(t)
	gr := segment.AverageGrade() / 100
	return calc.Psimp(calc.Rho(segment.MedianElevation(), calc.G), cda, calc.Crr, vg, vg, gr, mt, calc.G, calc.Ec, calc.Fw)
}

func getProgressOnGoals(goals []SegmentGoal, token string) ([]GoalProgress, error) {
	var progress GoalProgressList

	for _, goal := range goals {
		// Since Strava returns the efforts sorted by time, presumably one page
		// is enough to find the best efforts before and after.
		efforts, err := GetEfforts(goal.SegmentID, 1, token)
		if err != nil {
			return nil, err
		}

		bestEffort, _ := getBestEffortBefore(goal, efforts)
		bestAttempt, numAttempts := getBestAttemptAfter(goal, efforts)
		bestAttemptElapsedTime := -1
		var bestAttemptDuration string
		if bestAttempt != nil {
			bestAttemptElapsedTime = bestAttempt.ElapsedTime
			bestAttemptDuration = (time.Duration(bestAttempt.ElapsedTime) * time.Second).String()
		}

		p := GoalProgress{
			SegmentGoal:         goal,
			BestEffort:          bestEffort.ElapsedTime,
			BestEffortDuration:  (time.Duration(bestEffort.ElapsedTime) * time.Second).String(),
			NumEfforts:          len(efforts),
			BestAttempt:         bestAttemptElapsedTime,
			BestAttemptDuration: bestAttemptDuration,
			NumAttempts:         numAttempts,
			GoalDuration:        (time.Duration(goal.Goal) * time.Second).String(),
		}

		progress = append(progress, p)
	}

	progress = sortProgress(progress)
	return progress, nil
}

func getBestEffortBefore(goal SegmentGoal, efforts []*strava.SegmentEffortSummary) (*strava.SegmentEffortSummary, int) {
	var best *strava.SegmentEffortSummary
	num := 0
	date := time.Unix(0, int64(goal.Date)*(int64(time.Millisecond)/int64(time.Nanosecond)))

	for _, effort := range efforts {
		if effort.StartDateLocal.Before(date) {
			num++
			if best == nil || effort.ElapsedTime < best.ElapsedTime {
				best = effort
			}
		}
	}

	return best, num
}

func getBestAttemptAfter(goal SegmentGoal, efforts []*strava.SegmentEffortSummary) (*strava.SegmentEffortSummary, int) {
	var best *strava.SegmentEffortSummary
	num := 0
	date := time.Unix(0, int64(goal.Date)*(int64(time.Millisecond)/int64(time.Nanosecond)))

	for _, effort := range efforts {
		if effort.StartDateLocal.After(date) {
			num++
			if best == nil || effort.ElapsedTime < best.ElapsedTime {
				best = effort
			}
		}
	}

	return best, num
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

func sortProgress(pl GoalProgressList) GoalProgressList {
	sort.Sort(pl)
	return pl
}

type GoalProgressList []GoalProgress

func (p GoalProgressList) Len() int {
	return len(p)
}

func (p GoalProgressList) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

func (p GoalProgressList) Less(i, j int) bool {
	if p[i].NumAttempts == 0 {
		if p[j].NumAttempts == 0 {
			return (float64(p[i].Goal) / float64(p[i].BestEffort)) > (float64(p[j].Goal) / float64(p[j].BestEffort))
		} else {
			return false
		}

	}
	return (float64(p[i].Goal) / float64(p[i].BestAttempt)) > (float64(p[j].Goal) / float64(p[j].BestAttempt))
}
