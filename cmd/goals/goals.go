package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"math"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"os"
	"sort"
	"time"

	"github.com/scheibo/calc"
	"github.com/scheibo/perf"
	"github.com/scheibo/strava"
	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
	"github.com/scheibo/wnf"
	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/css"
	"github.com/tdewolff/minify/html"
	"github.com/tdewolff/minify/js"
	"github.com/tdewolff/minify/svg"
)

const WEEKDAY_BEGIN_HOUR = 7
const WEEKDAY_END_HOUR = 8

const WEEKEND_BEGIN_HOUR = 8
const WEEKEND_END_HOUR = 11

type SegmentGoal struct {
	// Name of the segment.
	Name string `json:"name"`
	// ID of the segment.
	SegmentID int64 `json:"segmentId"`
	// Target goal time in seconds for the segment.
	Time int `json:"time"`
	// Date after which attempts are tracked.
	Date int `json:"date"`
	// The actual segment.
	segment *Segment
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

func (s *SegmentGoal) Direction() string {
	return weather.Direction(s.segment.AverageDirection)
}

func (s *SegmentGoal) PWatts() float64 {
	return calcPower(s.Time, s.segment)
}

func (s *SegmentGoal) PERF() float64 {
	return PERF(s.Time, s.segment)
}

func (s *SegmentGoal) Perf() string {
	return fmt.Sprintf("%.0f", math.Round(s.PERF()))
}

func (s *SegmentGoal) PWatts2() string {
	return watts(s.PWatts())
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
	// The average forecasted weather conditions as of Date for the upcoming 'morning'.
	Forecast *weather.Conditions `json:"forecast,omitempty"`
	// The averaged baseline WNF score for the forecasted conditions as of Date
	// for the upcoming 'morning'.
	// NOTE: This is *not* the same as the WNF for Forecast!
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

func (p *GoalProgress) WWatts() float64 {
	return p.ForecastWNF * p.Goal.PWatts()
}

func (p *GoalProgress) WWatts2() string {
	return watts(p.WWatts())
}

func (p *GoalProgress) WPERF() float64 {
	return WPERF(p.WWatts(), p.Goal.Time, p.Goal.segment)
}

func (p *GoalProgress) Title() string {
	return fmt.Sprintf("%s (%.0f)\n%s", p.WWatts2(), math.Round(p.WPERF()), p.Weather())
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
	Watts float64 `json:"watts,omitempty"`
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
	// Whether or not to highlight this effort as the best.
	best bool
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
	return fmt.Sprintf("%.0f", math.Round(e.PERF))
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

func (e *Effort) Progress(t int) string {
	return fmt.Sprintf("%.2f%%", float64(t)/float64(e.Time)*100)
}

func (e *Effort) TimeTitle(t int) string {
	return fmt.Sprintf("%s\n%s", e.Day(), e.Progress(t))
}

func (e *Effort) Best() string {
	if e.best {
		return "best"
	}
	return ""
}

type C struct {
	reload  bool
	token   string
	climbs  *[]Climb
	patches map[int64]strava.DetailedSegmentEffort
	w       *weather.Client
	refresh time.Duration
	now     time.Time
	dir     string
}

func main() {
	now := time.Now()

	var reload bool
	var tz, key, token, output, goalsFile, patchesFile, climbsFile string
	var refresh time.Duration

	flag.BoolVar(&reload, "reload", false, "Perform a full reload instead of update.")
	flag.StringVar(&tz, "tz", "America/Los_Angeles", "timezone to use")
	flag.StringVar(&key, "key", os.Getenv("DARKSKY_API_KEY"), "DarkySky API Key")
	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&output, "output", "site", "Output directory")
	flag.StringVar(&goalsFile, "goals", "", "Goals")
	flag.StringVar(&patchesFile, "patch", "", "Patch to Strava segment efforts which are incorrect.")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")

	flag.DurationVar(&refresh, "refresh", 12*time.Hour,
		"minimum refresh interval for GoalProgress.Forecast")

	flag.Parse()

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

	var goals []GoalProgress
	err = json.Unmarshal(f, &goals)
	if err != nil {
		exit(err)
	}

	file = Resource("patch")
	if patchesFile != "" {
		file = patchesFile
	}

	f, err = ioutil.ReadFile(file)
	if err != nil {
		exit(err)
	}

	var ps []strava.DetailedSegmentEffort
	err = json.Unmarshal(f, &ps)
	if err != nil {
		exit(err)
	}

	patches := make(map[int64]strava.DetailedSegmentEffort)
	for _, p := range ps {
		patches[p.Id] = p
	}

	fi, _ := os.Stdin.Stat()
	if (fi.Mode() & os.ModeCharDevice) == 0 {
		bytes, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			exit(err)
		}

		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		for _, line := range strings.Split(strings.TrimSpace(string(bytes)), "\n") {
			// "name,id,time"
			s := strings.Split(line, ",")

			id, err := strconv.ParseInt(s[1], 0, 64)
			if err != nil {
				exit(err)
			}

			t, err := time.ParseDuration(s[2])
			if err != nil {
				exit(err)
			}

			goal := SegmentGoal{
				Name:      s[0],
				SegmentID: id,
				Time:      int(t.Seconds()),
				Date:      int(today.Unix() * 1000),
			}
			goals = append(goals, GoalProgress{Goal: goal})
		}
	}

	c := C{
		reload:  reload,
		token:   token,
		climbs:  &climbs,
		patches: patches,
		w:       weather.NewClient(weather.DarkSky(key), weather.TimeZone(loc)),
		refresh: refresh,
		now:     now,
		dir:     output,
	}

	progress, err := c.update(goals)
	if err != nil {
		exit(err)
	}

	err = c.render(progress)
	if err != nil {
		exit(err)
	}

	j, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		exit(err)
	}

	err = ioutil.WriteFile(filepath.Join(c.dir, "goals.json"), j, 0644)
	if err != nil {
		exit(err)
	}
}

func (c *C) update(prev []GoalProgress) ([]GoalProgress, error) {
	var progress GoalProgressList

	for _, p := range prev {
		goal := p.Goal
		efforts, err := GetEfforts(goal.SegmentID, 0, c.token)
		if err != nil {
			return nil, err
		}

		segment, err := GetSegmentByID(goal.SegmentID, *c.climbs, c.token)
		if err != nil {
			return nil, err
		}
		goal.segment = segment

		date := goal.Date
		if !c.reload && p.Date > date {
			date = p.Date
		}

		best := p.BestAttempt
		attempts := p.NumAttempts
		if c.reload {
			best = nil
			attempts = 0
		}
		bestAttempt, numAttempts, err := c.getBestAttemptAfter(
			fromEpochMillis(date), goal, efforts, best)
		if err != nil {
			return nil, err
		}
		numAttempts += attempts

		bestEffort, numEfforts := p.BestEffort, p.NumEfforts
		if c.reload || bestEffort == nil {
			best, numEffortsBefore, err := c.getBestEffortBefore(
				fromEpochMillis(goal.Date), goal, efforts)
			if err != nil {
				return nil, err
			}
			bestEffort = best
			numEfforts = numEffortsBefore + numAttempts
		}

		if bestAttempt != nil {
			if bestEffort != nil && bestEffort.Time < bestAttempt.Time {
				bestEffort.best = true
			} else {
				bestAttempt.best = true
			}
		} else {
			if bestEffort != nil {
				bestEffort.best = true
			}
		}

		forecast, forecastWNF := p.Forecast, p.ForecastWNF
		if c.reload || forecast == nil || c.now.Sub(fromEpochMillis(p.Date)) > c.refresh {
			forecasts, err := c.getForecast(segment)
			if err != nil {
				return nil, err
			}

			forecast = weather.Average(forecasts)
			// NOTE: The forecastWNF is not computed simply as the WNF of the average
			// forecast - instead we compute the WNF for each forecast and average the
			// result.
			forecastWNF := 0.0
			for _, f := range forecasts {
				fWNF, _, err := PowerWNF(goal.PWatts(), segment, f, nil /* past */)
				if err != nil {
					return nil, err
				}
				forecastWNF += fWNF
			}
			forecastWNF /= float64(len(forecasts))
		}

		u := GoalProgress{
			Date:        int(c.now.Unix() * 1000),
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

func (c *C) getBestEffortBefore(
	date time.Time,
	goal SegmentGoal,
	efforts []strava.DetailedSegmentEffort) (*Effort, int, error) {

	return c.getBestEffort(func(ed, gd time.Time) bool {
		return ed.Before(gd)
	}, date, goal, efforts, nil)
}

func (c *C) getBestAttemptAfter(
	date time.Time,
	goal SegmentGoal,
	efforts []strava.DetailedSegmentEffort,
	prev *Effort) (*Effort, int, error) {

	return c.getBestEffort(func(ed, gd time.Time) bool {
		return ed.After(gd)
	}, date, goal, efforts, prev)
}

func (c *C) getBestEffort(
	fun func(ed, gd time.Time) bool,
	date time.Time,
	goal SegmentGoal,
	efforts []strava.DetailedSegmentEffort,
	prev *Effort) (*Effort, int, error) {

	var best *strava.DetailedSegmentEffort
	num := 0

	for _, effort := range efforts {
		e := effort
		if p, ok := c.patches[e.Id]; ok {
			e = p
		}

		if fun(e.StartDate, date) {
			num++
			if best == nil || e.ElapsedTime < best.ElapsedTime {
				best = &e
			}
		}
	}

	if best == nil || (prev != nil && !(int(best.ElapsedTime) < prev.Time)) {
		return prev, num, nil
	}

	e, err := c.toEffort(best, goal.segment)
	if err != nil {
		return nil, 0, err
	}

	return e, num, nil
}

func (c *C) toEffort(s *strava.DetailedSegmentEffort, segment *Segment) (*Effort, error) {
	e := Effort{}
	e.ActivityID = s.Activity.Id
	e.EffortID = s.Id
	e.Date = int(s.StartDate.Unix() * 1000)
	e.Time = int(s.ElapsedTime)
	if s.DeviceWatts {
		e.Watts = float64(s.AverageWatts)
	} else {
		e.Watts = -1
	}
	e.PERF = PERF(e.Time, segment)
	e.PWatts = calcPower(e.Time, segment)

	w, err := c.w.History(segment.AverageLocation, s.StartDate)
	if err != nil {
		return nil, err
	}
	e.Conditions = w

	// NOTE: using PWatts for consistency as opposed to AveragePower
	baseline, _, err := PowerWNF(e.PWatts, segment, w, nil /* past */)
	if err != nil {
		return nil, err
	}
	e.WNF = baseline
	e.WWatts = baseline * e.PWatts

	return &e, nil
}

func (c *C) getForecast(segment *Segment) ([]*weather.Conditions, error) {
	f, err := c.w.Forecast(segment.AverageLocation)
	if err != nil {
		return nil, err
	}

	// The upcoming forecast is for the current day unless we're already past
	// the end hour, in which case we use the conditions for tomorrow.
	t := c.now
	if t.Hour() > WEEKDAY_END_HOUR &&
		!(weekend(t) && t.Hour() <= WEEKEND_END_HOUR) {
		t = t.AddDate(0, 0, 1)
	}

	begin, end := WEEKDAY_BEGIN_HOUR, WEEKDAY_END_HOUR
	if weekend(t) {
		begin, end = WEEKEND_BEGIN_HOUR, WEEKEND_END_HOUR
	}

	var cs []*weather.Conditions
	for _, h := range f.Hourly {
		if h.Time.YearDay() == t.YearDay() &&
			h.Time.Hour() >= begin && h.Time.Hour() <= end {
			cs = append(cs, h)
		}
	}
	return cs, nil
}

func (c *C) render(goals []GoalProgress) error {
	err := os.RemoveAll(c.dir)
	if err != nil {
		return err
	}
	err = copyFile(resource("favicon.ico"), filepath.Join(c.dir, "favicon.ico"))
	if err != nil {
		return err
	}
	f, err := create(filepath.Join(c.dir, "index.html"))
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
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

func fromEpochMillis(millis int) time.Time {
	return time.Unix(0, int64(millis)*(int64(time.Millisecond)/int64(time.Nanosecond)))
}

func weekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
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

func PERF(t int, segment *Segment) float64 {
	return perf.Calc(
		float64(t),
		segment.Distance,
		segment.AverageGrade,
		segment.MedianElevation,
		wnf.Mr,
		cda(segment),
		perf.CpM)
}

func WPERF(p float64, t int, segment *Segment) float64 {
	return perf.Score(p, calc.AltitudeAdjust(perf.CpM(float64(t)), segment.MedianElevation))
}

func calcPower(t int, segment *Segment) float64 {
	vg := segment.Distance / float64(t)

	return calc.Psimp(
		calc.Rho(segment.MedianElevation, calc.G),
		cda(segment), calc.Crr, vg, vg, segment.AverageGrade, wnf.Mt, calc.G, calc.Ec, calc.Fw)
}

func cda(segment *Segment) float64 {
	cda := wnf.CdaClimb
	if segment.AverageGrade < CLIMB_THRESHOLD {
		cda = wnf.CdaTT
	}
	return cda
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
	return p[i].WPERF() < p[j].WPERF()

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

		mb, err := m.Bytes("text/html", b)
		if err != nil {
			return nil, err
		}
		_, err = tmpl.Parse(string(mb))
		if err != nil {
			return nil, err
		}
	}
	return tmpl, nil
}
