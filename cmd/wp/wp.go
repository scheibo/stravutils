package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"html/template"

	"github.com/scheibo/perf"
	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
	"github.com/scheibo/wnf"
	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/css"
	"github.com/tdewolff/minify/html"
	"github.com/tdewolff/minify/js"
	"github.com/tdewolff/minify/svg"
)

const msToKmh = 3600.0 / 1000.0

const minHour = 6
const maxHour = 18

func main() {
	var key, climbsFile string
	var min, max int

	flag.StringVar(&key, "key", "", "DarkySky API Key")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.IntVar(&min, "min", 6, "Minimum hour [0-23] to include in forecasts")
	flag.IntVar(&max, "max", 18, "Maximum hour [0-23] to include in forecasts")

	flag.Parse()

	climbs, err := GetClimbs(climbsFile)
	if err != nil {
		exit(err)
	}

	w := weather.NewClient(weather.DarkSky(key))

	var forecasts []*ClimbForecast
	for _, climb := range climbs {
		f, err := w.Forecast(climb.Segment.AverageLocation)
		if err != nil {
			exit(err)
		}
		c := climb
		forecasts = append(forecasts, trimAndScore(&c, f, min, max))
	}

	// TODO(kjs): handle nested pages
	t := template.Must(compileTemplates(resource("wp.html")))
	err = t.Execute(os.Stdout, struct {
		Forecasts []*ClimbForecast
	}{
		forecasts,
	})

	if err != nil {
		exit(err)
	}
}

type ScoredConditions struct {
	*weather.Conditions
	score float64
}

type DayForecast struct {
	Day        string
	Conditions []*ScoredConditions
}

type ScoredForecast struct {
	Days []*DayForecast
	Best *ScoredConditions
}

type ClimbForecast struct {
	Climb    *Climb
	Forecast *ScoredForecast
}

func (f *ClimbForecast) ClimbDirection() string {
	return weather.Direction(f.Climb.Segment.AverageDirection)
}

func (f *ClimbForecast) Current() *ScoredConditions {
	return f.Forecast.Days[0].Conditions[0]
}

func trimAndScore(c *Climb, f *weather.Forecast, min, max int) *ClimbForecast {
	scored := ScoredForecast{}
	result := &ClimbForecast{Climb: c, Forecast: &scored}
	if len(f.Hourly) == 0 {
		return result
	}

	df := DayForecast{}
	for _, w := range f.Hourly {
		hours, _, _ := w.Time.Clock()
		if hours < min || hours > max {
			continue
		}

		s := score(c, w)
		day := s.Day()
		if df.Day == "" {
			df.Day = day
		} else if day != df.Day {
			scored.Days = append(scored.Days, &df)
			df = DayForecast{Day: day}
		}
		df.Conditions = append(df.Conditions, s)
		if scored.Best == nil || s.score > scored.Best.score {
			scored.Best = s
		}
	}
	if df.Day != "" {
		scored.Days = append(scored.Days, &df)
	}

	return result
}

func score(climb *Climb, conditions *weather.Conditions) *ScoredConditions {
	// TODO(kjs): use c.Map polyline for more accurate score.
	score := wnf.Power(
		perf.CalcPowerM(500, climb.Segment.Distance, climb.Segment.AverageGrade, climb.Segment.MedianElevation),
		climb.Segment.Distance,
		climb.Segment.MedianElevation,
		conditions.AirDensity,
		wnf.CdaClimb,
		conditions.WindSpeed,
		conditions.WindBearing,
		climb.Segment.AverageDirection,
		climb.Segment.AverageGrade,
		wnf.Mr+wnf.Mb)
	return &ScoredConditions{conditions, score}
}

func (c *ScoredConditions) Rank() int {
	mod := 1
	if c.score < 1.0 {
		mod = -1
	}

	rank := int(math.Abs(c.score-1) * 100)
	if rank > 5 {
		rank = 5
	}

	return mod * rank
}

func (c *ScoredConditions) Score() string {
	return fmt.Sprintf("%.2f%%", (c.score-1)*100)
}

func (c *ScoredConditions) Wind() string {
	return fmt.Sprintf("%.1f km/h %s", c.WindSpeed, weather.Direction(c.WindBearing))
}

func (c *ScoredConditions) Day() string {
	return c.Time.Format("Monday")
}

func (c *ScoredConditions) DayTime() string {
	return c.Time.Format("Monday 3PM")
}

func resource(filename string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), filename)
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

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
