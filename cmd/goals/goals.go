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
	"github.com/scheibo/weather"
	"github.com/scheibo/wnf"
	"github.com/strava/go.strava"
)

type SegmentGoal struct {
	// Name of the segment.
	Name string `json:"name"`
	// ID of the segment.
	SegmentID int64 `json:"segmentId"`
	// Target goal time in seconds for the segment.
	Time int `json:"time"`
	// Date after which attempts are tracked.
	Date int `json:"date"`
}

type GoalProgress struct {
	// Milliseconds since the epoch the data for this struct was calculated.
	Date int `json:"date"`
	// The particular SegmentGoal to track progress against.
	Goal SegmentGoal `json:"goal"`
	// The best effort for the segment before Goal.Date.
	// ie. PR time when the goal was set.
	BestEffort *Effort `json:"bestEffort"`
	// The total number of efforts for the segment, irrespective of date.
	NumEfforts int `json:"numEfforts"`
	// The best effort for the segment after Goal.Date.
	BestAttempt *Effort `json:"bestAttempt,omitempty"`
	// The total number of efforts for the segment after Goal.Date.
	NumAttempts int `json:"numAttempts"` // total attempts after Goal.Date
	// The forecasted weather conditions as of Date for the upcoming Saturday @ 10AM.
	Forecast *weather.Conditions `json:"forecast"`
	// The baseline WNF score for the Forecast conditions.
	ForecastWNF float64 `json:"wnf"`
}

type Effort struct {
	// ID of the effort.
	ID int64 `json:"id"`
	// Millisseconds since the epoch the effort occurred at.
	Date int `json:"date"`
	// The time of the effort in seconds.
	Time int `json:"time"`
	// The average watts measured for the effort.
	Watts float64 `json:"watts"`
	// The PERF score for the effort.
	PERF float64 `json:"perf"`
	// The predicted average watts for the effort according to PERF.
	PWatts float64 `json:"pwatts"`
	// The baseline WNF score for the effort (given Conditions).
	WNF float64 `json:"wnf"`
	// The predicted average watts for the effort according to PERF, given Conditions.
	WWatts float64 `json:"wwatts"`
	// The weather conditions for the effort.
	Conditions *weather.Conditions `json:"weather"`
}

type C struct {
	cda, mt float64
	token   string
	climbs  *[]Climb
	w       *weather.Client
	refresh time.Duration
}

func main() {
	var key, token, goalsFile, climbsFile string
	var cda, mr, mb float64
	var refresh time.Duration

	flag.StringVar(&key, "key", os.Getenv("DARKSKY_API_KEY"), "DarkySky API Key")
	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&goalsFile, "goals", "", "Goals")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")

	flag.Float64Var(&cda, "cda", wnf.CdaClimb, "coefficient of drag area")
	flag.Float64Var(&mr, "mr", wnf.Mr, "total mass of the rider in kg")
	flag.Float64Var(&mb, "mb", wnf.Mb, "total mass of the bicycle in kg")

	flag.DurationVar(&refresh, "refresh", 12*time.Hour, "minimum refresh interval for GoalProgress.Forecast")

	flag.Parse()

	verify("cda", cda)
	verify("mr", mr)
	verify("mb", mb)

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

	c := C{
		cda:     cda,
		mt:      mr + mb,
		token:   token,
		climbs:  &climbs,
		w:       weather.NewClient(weather.DarkSky(key)),
		refresh: refresh,
	}

	var goals []GoalProgress
	//err = json.Unmarshal(f, &goals)
	//if err != nil {
	//exit(err)
	//}

	// TODO: just marshall directly (above) instead.
	err = getGoalProgress(f, &goals)
	if err != nil {
		exit(err)
	}

	progress, err := c.update(goals)
	if err != nil {
		exit(err)
	}

	j, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		exit(err)
	}
	fmt.Println(string(j))
}

func getGoalProgress(f []byte, progress *[]GoalProgress) error {
	var goals []SegmentGoal
	err := json.Unmarshal(f, &goals)
	if err != nil {
		return err
	}
	for _, goal := range goals {
		p := GoalProgress{Goal: goal}
		*progress = append(*progress, p)
	}
	return nil
}

func (c *C) update(prev []GoalProgress) ([]GoalProgress, error) {
	var progress GoalProgressList

	now := time.Now()
	for _, p := range prev {
		goal := p.Goal
		// Since Strava returns the efforts sorted by time, presumably one page
		// is enough to find the best efforts before and after.
		efforts, err := GetEfforts(goal.SegmentID, 1, c.token)
		if err != nil {
			return nil, err
		}

		segment, err := GetSegmentByID(goal.SegmentID, *c.climbs, c.token)
		if err != nil {
			return nil, err
		}

		date := goal.Date
		if p.Date > date {
			date = p.Date
		}

		bestAttempt, numAttempts, err := c.getBestAttemptAfter(fromEpochMillis(date), goal, efforts, segment, p.BestAttempt)
		if err != nil {
			return nil, err
		}
		bestEffort, numEfforts := p.BestEffort, p.NumEfforts
		if bestEffort == nil {
			best, numEffortsBefore, err := c.getBestEffortBefore(fromEpochMillis(goal.Date), goal, efforts, segment)
			if err != nil {
				return nil, err
			}
			bestEffort = best
			numEfforts = numEffortsBefore + numAttempts
		}

		forecast, forecastWNF := p.Forecast, p.ForecastWNF
		if forecast == nil || fromEpochMillis(p.Date).Sub(now) > c.refresh {
			forecast, err := c.getForecast(segment, now)
			if err != nil {
				return nil, err
			}
			forecastWNF, _, err = WNF(segment, forecast, nil /* past */)
			if err != nil {
				return nil, err
			}
		}

		u := GoalProgress{
			Date:        int(now.Unix() * int64(time.Millisecond)),
			Goal:        goal,
			BestEffort:  bestEffort,
			NumEfforts:  numEfforts,
			BestAttempt: bestAttempt,
			NumAttempts: numAttempts,
			Forecast:    forecast,
			ForecastWNF: forecastWNF,
		}

		progress = append(progress, u)
	}

	progress = sortProgress(progress)
	return progress, nil
}

func (c *C) getBestEffortBefore(date time.Time, goal SegmentGoal, efforts []*strava.SegmentEffortSummary, segment *Segment) (*Effort, int, error) {
	return c.getBestEffort(func(ed, gd time.Time) bool { return ed.Before(gd) }, date, goal, efforts, segment, nil)
}

func (c *C) getBestAttemptAfter(date time.Time, goal SegmentGoal, efforts []*strava.SegmentEffortSummary, segment *Segment, prev *Effort) (*Effort, int, error) {
	return c.getBestEffort(func(ed, gd time.Time) bool { return ed.After(gd) }, date, goal, efforts, segment, prev)
}

func (c *C) getBestEffort(fun func(ed, gd time.Time) bool, date time.Time, goal SegmentGoal, efforts []*strava.SegmentEffortSummary, segment *Segment, prev *Effort) (*Effort, int, error) {
	if prev != nil {
		date = fromEpochMillis(prev.Date)
	}

	var best *strava.SegmentEffortSummary
	num := 0

	for _, effort := range efforts {
		if fun(effort.StartDate, date) {
			num++
			if best == nil || effort.ElapsedTime < best.ElapsedTime {
				best = effort
			}
		}
	}

	if best == nil || !(best.ElapsedTime < prev.Time) {
		return prev, num, nil
	}

	e, err := c.toEffort(best, segment)
	if err != nil {
		return nil, 0, err
	}

	return e, num, nil
}

func (c *C) toEffort(s *strava.SegmentEffortSummary, segment *Segment) (*Effort, error) {
	e := Effort{}
	e.Date = int(s.StartDate.Unix() * int64(time.Millisecond))
	e.Time = s.ElapsedTime
	e.Watts = s.AveragePower
	e.PERF = perf.CalcM(float64(s.ElapsedTime), segment.Distance, segment.AverageGrade, segment.MedianElevation)
	e.PWatts = c.calcPower(s.ElapsedTime, segment)

	w, err := c.w.History(segment.AverageLocation, s.StartDate)
	if err != nil {
		return nil, err
	}
	e.Conditions = w

	baseline, _, err := WNF(segment, w, nil /* past */)
	if err != nil {
		return nil, err
	}
	e.WNF = baseline
	e.WWatts = baseline * e.PWatts

	return &e, nil
}

func (c *C) calcPower(t int, segment *Segment) float64 {
	vg := segment.Distance / float64(t)
	return calc.Psimp(calc.Rho(segment.MedianElevation, calc.G), c.cda, calc.Crr, vg, vg, segment.AverageGrade, c.mt, calc.G, calc.Ec, calc.Fw)
}

// TODO(kjs): average forecasts around 10am isntead of returning CURRENT
func (c *C) getForecast(segment *Segment, now time.Time) (*weather.Conditions, error) {
	return c.w.Current(segment.AverageLocation)
}

func fromEpochMillis(millis int) time.Time {
	return time.Unix(0, int64(millis)*(int64(time.Millisecond)/int64(time.Nanosecond)))
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
			return (float64(p[i].Goal.Time) / float64(p[i].BestEffort.Time)) > (float64(p[j].Goal.Time) / float64(p[j].BestEffort.Time))
		} else {
			return false
		}

	}
	return (float64(p[i].Goal.Time) / float64(p[i].BestAttempt.Time)) > (float64(p[j].Goal.Time) / float64(p[j].BestAttempt.Time))
}
