package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"math"
	"path/filepath"
	"runtime"

	"os"
	"sort"
	"time"

	"github.com/scheibo/calc"
	"github.com/scheibo/perf"
	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
	"github.com/scheibo/wnf"
	"github.com/strava/go.strava"
	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/css"
	"github.com/tdewolff/minify/html"
	"github.com/tdewolff/minify/js"
	"github.com/tdewolff/minify/svg"
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

func (s *SegmentGoal) SegmentURL() string {
	return fmt.Sprintf("https://www.strava.com/segments/%d", s.SegmentID)
}

func (s *SegmentGoal) Duration() string {
	return duration(s.Time)
}

func (s *SegmentGoal) Day() string {
	return day(s.Date)
}

type GoalProgress struct {
	// Milliseconds since the epoch the data for this struct was calculated.
	Date int `json:"date"`
	// The particular SegmentGoal to track progress against.
	Goal SegmentGoal `json:"goal"`
	// The best effort for the segment before Goal.Date.
	// ie. PR time when the goal was set.
	BestEffort *Effort `json:"bestEffort,omitempty"`
	// The total number of efforts for the segment, irrespective of date.
	NumEfforts int `json:"numEfforts"`
	// The best effort for the segment after Goal.Date.
	BestAttempt *Effort `json:"bestAttempt,omitempty"`
	// The total number of efforts for the segment after Goal.Date.
	NumAttempts int `json:"numAttempts"` // total attempts after Goal.Date
	// The forecasted weather conditions as of Date for the upcoming Saturday @ 10AM.
	Forecast *weather.Conditions `json:"forecast,omitempty"`
	// The baseline WNF score for the Forecast conditions.
	ForecastWNF float64 `json:"wnf"`
}

func (p *GoalProgress) Day() string {
	return day(p.Date)
}

func (p *GoalProgress) Weather() string {
	return weatherString(p.Forecast)
}

func (p *GoalProgress) Score() string {
	return score(p.ForecastWNF)
}

func (p *GoalProgress) Rank() int {
	return rank(p.ForecastWNF)
}

type Effort struct {
	// ID of the activity the effort took place in.
	ActivityID int64 `json:"activityId"`
	// ID of the effort.
	EffortID int64 `json:"effortId"`
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
	Conditions *weather.Conditions `json:"weather,omitempty"`
}

func (e *Effort) URL() string {
	return fmt.Sprintf("https://www.strava.com/activities/%d/segments/%d", e.ActivityID, e.EffortID)
}

func (e *Effort) Day() string {
	return day(e.Date)
}

func (e *Effort) Duration() string {
	return duration(e.Time)
}

func (e *Effort) Perf() string {
	return fmt.Sprintf("%.2f", e.PERF)
}

func (e *Effort) Watts2() string {
	return watts(e.Watts)
}

func (e *Effort) PWatts2() string {
	return watts(e.PWatts)
}

func (e *Effort) WWatts2() string {
	return watts(e.WWatts)
}

func (e *Effort) Score() string {
	return score(e.WNF)
}

func (e *Effort) Rank() int {
	return rank(e.WNF)
}

func (e *Effort) Weather() string {
	return weatherString(e.Conditions)
}

func (e *Effort) Title() string {
	return fmt.Sprintf("%s\n%s", e.WWatts2(), e.Weather())
}

type C struct {
	cda, mt float64
	token   string
	climbs  *[]Climb
	w       *weather.Client
	refresh time.Duration
	now     time.Time
}

func main() {
	now := time.Now()

	var tz, key, token, goalsFile, climbsFile string
	var cda, mr, mb float64
	var refresh time.Duration

	flag.StringVar(&tz, "tz", "America/Los_Angeles", "timezone to use")
	flag.StringVar(&key, "key", os.Getenv("DARKSKY_API_KEY"), "DarkySky API Key")
	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&goalsFile, "goals", "", "Goals")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")

	flag.Float64Var(&cda, "cda", wnf.CdaClimb, "coefficient of drag area")
	flag.Float64Var(&mr, "mr", wnf.Mr, "total mass of the rider in kg")
	flag.Float64Var(&mb, "mb", wnf.Mb, "total mass of the bicycle in kg")

	flag.DurationVar(&refresh, "refresh", 12*time.Hour,
		"minimum refresh interval for GoalProgress.Forecast")

	flag.Parse()

	verify("cda", cda)
	verify("mr", mr)
	verify("mb", mb)

	loc, err := time.LoadLocation(tz)
	if err != nil {
		exit(err)
	}

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
		w:       weather.NewClient(weather.DarkSky(key), weather.TimeZone(loc)),
		refresh: refresh,
		now:     now,
	}

	var goals []GoalProgress
	err = json.Unmarshal(f, &goals)
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

	err = c.render(goals)
	if err != nil {
		exit(err)
	}
}

func (c *C) update(prev []GoalProgress) ([]GoalProgress, error) {
	var progress GoalProgressList

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

		bestAttempt, numAttempts, err := c.getBestAttemptAfter(
			fromEpochMillis(date), goal, efforts, segment, p.BestAttempt)
		if err != nil {
			return nil, err
		}
		bestEffort, numEfforts := p.BestEffort, p.NumEfforts
		if bestEffort == nil {
			best, numEffortsBefore, err := c.getBestEffortBefore(
				fromEpochMillis(goal.Date), goal, efforts, segment)
			if err != nil {
				return nil, err
			}
			bestEffort = best
			numEfforts = numEffortsBefore + numAttempts
		}

		forecast, forecastWNF := p.Forecast, p.ForecastWNF
		if forecast == nil || c.now.Sub(fromEpochMillis(p.Date)) > c.refresh {
			forecast, err = c.getForecast(segment)
			if err != nil {
				return nil, err
			}
			forecastWNF, _, err = WNF(segment, forecast, nil /* past */)
			if err != nil {
				return nil, err
			}
		}

		u := GoalProgress{
			Date:        int(c.now.Unix() * 1000),
			Goal:        goal,
			BestEffort:  bestEffort,
			NumEfforts:  numEfforts,
			BestAttempt: bestAttempt,
			NumAttempts: p.NumAttempts + numAttempts,
			Forecast:    forecast,
			ForecastWNF: forecastWNF,
		}

		progress = append(progress, u)
	}

	progress = sortProgress(progress)
	return progress, nil
}

func (c *C) getBestEffortBefore(
	date time.Time,
	goal SegmentGoal,
	efforts []*strava.SegmentEffortSummary,
	segment *Segment) (*Effort, int, error) {

	return c.getBestEffort(func(ed, gd time.Time) bool {
		return ed.Before(gd)
	}, date, goal, efforts, segment, nil)
}

func (c *C) getBestAttemptAfter(
	date time.Time,
	goal SegmentGoal,
	efforts []*strava.SegmentEffortSummary,
	segment *Segment,
	prev *Effort) (*Effort, int, error) {

	return c.getBestEffort(func(ed, gd time.Time) bool {
		return ed.After(gd)
	}, date, goal, efforts, segment, prev)
}

func (c *C) getBestEffort(
	fun func(ed, gd time.Time) bool,
	date time.Time,
	goal SegmentGoal,
	efforts []*strava.SegmentEffortSummary,
	segment *Segment, prev *Effort) (*Effort, int, error) {

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

	if best == nil || (prev != nil && !(best.ElapsedTime < prev.Time)) {
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
	e.ActivityID = s.Activity.Id
	e.EffortID = s.Id
	e.Date = int(s.StartDate.Unix() * 1000)
	e.Time = s.ElapsedTime
	e.Watts = s.AveragePower
	e.PERF = perf.CalcM(
		float64(s.ElapsedTime), segment.Distance, segment.AverageGrade, segment.MedianElevation)
	e.PWatts = c.calcPower(s.ElapsedTime, segment)

	fmt.Fprintf(os.Stderr, "%s %s %d %s\n", s.StartDate, s.StartDateLocal, e.Date, fromEpochMillis(e.Date))

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
	return calc.Psimp(
		calc.Rho(segment.MedianElevation, calc.G),
		c.cda, calc.Crr, vg, vg, segment.AverageGrade, c.mt, calc.G, calc.Ec, calc.Fw)
}

// BUG: This won't be accurate if the script is run on Saturday between 8 and 12.
func (c *C) getForecast(segment *Segment) (*weather.Conditions, error) {
	f, err := c.w.Forecast(segment.AverageLocation)
	if err != nil {
		return nil, err
	}
	var cs []*weather.Conditions
	for _, h := range f.Hourly {
		if h.Time.Weekday() == time.Saturday &&
			h.Time.Hour() >= 8 && h.Time.Hour() <= 11 {
			cs = append(cs, h)
		}
	}
	return weather.Average(cs), nil
}

func (c *C) render(goals []GoalProgress) error {
	f, err := create("goals.html")
	if err != nil {
		return err
	}

	t := template.Must(compileTemplates(resource("goals.tmpl.html")))
	return t.Execute(f, struct {
		GenerationTime string
		Goals          []GoalProgress
	}{
		c.now.Format(time.Stamp),
		goals,
	})
}

func create(path string) (*os.File, error) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
}

func fromEpochMillis(millis int) time.Time {
	return time.Unix(0, int64(millis)*(int64(time.Millisecond)/int64(time.Nanosecond)))
}

func duration(t int) string {
	d := time.Duration(t) * time.Second

	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func day(t int) string {
	return fromEpochMillis(t).Format("2 Jan 2006")
}

func score(s float64) string {
	return fmt.Sprintf("%.2f%%", (s-1)*100)
}

func rank(s float64) int {
	mod := 1
	if s > 1.0 {
		mod = -1
	}

	rank := int(math.Abs(s-1) * 100)
	if rank < 1 {
		rank = 0
	} else if rank >= 1 && rank < 3 {
		rank = 1
	} else if rank >= 3 && rank < 6 {
		rank = 2
	} else if rank >= 6 && rank < 10 {
		rank = 3
	} else if rank >= 10 && rank < 15 {
		rank = 4
	} else {
		rank = 5
	}

	return mod * rank
}

func watts(w float64) string {
	return fmt.Sprintf("%.0fW", math.Round(w))
}

func weatherString(c *weather.Conditions) string {
	precip := ""
	if c.PrecipProbability > 0.1 {
		precip = c.Precip()
	}
	return fmt.Sprintf("%.1f°C (%.3f kg/m³)\n%s%s", c.Temperature, c.AirDensity, precip, c.Wind())
}

func verify(s string, x float64) {
	if x < 0 {
		exit(fmt.Errorf("%s must be non negative but was %f", s, x))
	}
}

func resource(name string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), name)
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
			return (float64(p[i].Goal.Time) / float64(p[i].BestEffort.Time)) >
				(float64(p[j].Goal.Time) / float64(p[j].BestEffort.Time))
		} else {
			return false
		}
	}
	if p[j].NumAttempts == 0 {
		return true
	}

	return (float64(p[i].Goal.Time) / float64(p[i].BestAttempt.Time)) >
		(float64(p[j].Goal.Time) / float64(p[j].BestAttempt.Time))
}

func compileTemplates(filenames ...string) (*template.Template, error) {
	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/html", html.Minify)
	m.AddFunc("text/javascript", js.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)

	var tmpl *template.Template
	for _, filename := range filenames {
		name := filepath.Base(filename)
		if tmpl == nil {
			tmpl = template.New(name)
		} else {
			tmpl = tmpl.New(name)
		}

		b, err := ioutil.ReadFile(filename)
		if err != nil {
			return nil, err
		}

		//mb, err := m.Bytes("text/html", b)
		//if err != nil {
		//return nil, err
		//}
		_, err = tmpl.Parse(string(b)) // TODO mb
		if err != nil {
			return nil, err
		}
	}
	return tmpl, nil
}
