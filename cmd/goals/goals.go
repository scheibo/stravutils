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

	"github.com/strava/go.strava"
)

const MAX_PER_PAGE = 200

type SegmentGoal struct {
	Name      string `json:"name"`
	SegmentId int64  `json:"segmentId"`
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
	var accessToken string
	var goalsFile string

	// Provide an access token, with write permissions.
	// You'll need to complete the oauth flow to get one.
	flag.StringVar(&accessToken, "token", "", "Access Token")
	flag.StringVar(&goalsFile, "goals", "", "Goals")

	flag.Parse()

	if accessToken == "" {
		fmt.Println("\nPlease provide an access_token, one can be found at https://www.strava.com/settings/api")

		flag.PrintDefaults()
		os.Exit(1)
	}

	if goalsFile == "" {
		fmt.Println("\nPlease provide a JSON file name pointing towards goals.")

		flag.PrintDefaults()
		os.Exit(1)
	}

	file, err := ioutil.ReadFile(goalsFile)
	maybeExit(err)
	var goals []SegmentGoal
	err = json.Unmarshal(file, &goals)
	maybeExit(err)

	client := strava.NewClient(accessToken)
	currentAthleteId, err := getCurrentAthleteId(client)
	maybeExit(err)

	progress, err := getProgressOnGoals(client, goals, currentAthleteId)
	maybeExit(err)

	for _, goal := range progress {
		fmt.Printf("%s: %v -> %v (%.2f%%)",
			goal.Name,
			goal.BestEffortDuration,
			goal.GoalDuration,
			float64(goal.Goal)*100.0/float64(goal.BestEffort))
		if goal.NumAttempts > 0 {
			fmt.Printf(" => %v (%.2f%%)",
				goal.BestAttemptDuration,
				float64(goal.Goal)*100.0/float64(goal.BestAttempt))
		}
		fmt.Printf(" [%v/%v]\n", goal.NumAttempts, goal.NumEfforts)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.Encode(progress)
}

func maybeExit(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getCurrentAthleteId(client *strava.Client) (int64, error) {
	service := strava.NewCurrentAthleteService(client)
	athlete, err := service.Get().Do()
	if err != nil {
		return -1, err
	}
	return athlete.Id, nil
}

func getProgressOnGoals(client *strava.Client, goals []SegmentGoal, currentAthleteId int64) ([]GoalProgress, error) {
	service := strava.NewSegmentsService(client)

	var progress GoalProgressList

	for _, goal := range goals {
		// TODO get all, even if more than MAX_PER_PAGE
		efforts, err := service.ListEfforts(goal.SegmentId).
			PerPage(MAX_PER_PAGE).
			AthleteId(currentAthleteId).
			Do()
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
