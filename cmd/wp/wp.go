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

	"github.com/scheibo/calc"
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
	var output, key, climbsFile string
	var min, max int

	flag.StringVar(&output, "output", "weather-panel", "Output directory")
	flag.StringVar(&key, "key", "", "DarkySky API Key")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.IntVar(&min, "min", 6, "Minimum hour [0-23] to include in forecasts")
	flag.IntVar(&max, "max", 18, "Maximum hour [0-23] to include in forecasts")

	flag.Parse()

	if min < 0 || max > 23 || min >= max {
		exit(fmt.Errorf("min and max must be in the range [0-23] with min < max but got min=%d max=%d", min, max))
	}

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
		cf, err := trimAndScore(&c, f, min, max)
		if err != nil {
			exit(err)
		}
		forecasts = append(forecasts, cf)
	}

	err = render(output, forecasts)
	if err != nil {
		exit(err)
	}
}

func render(dir string, forecasts []*ClimbForecast) error {
	err := os.MkdirAll(dir, 0644)
	if err != nil {
		return err
	}

	// TODO(kjs): handle nested pages
	file, err := os.Create(filepath.Join(dir, "index.html"))
	if err != nil {
		return err
	}

	t := template.Must(compileTemplates(resource("wp.html")))
	err = t.Execute(file, struct {
		Forecasts []*ClimbForecast
	}{
		forecasts,
	})
	file.Close()

	return err
}

type ScoredConditions struct {
	*weather.Conditions
	historical float64
	baseline   float64
}

type DayForecast struct {
	Day        string
	Conditions []*ScoredConditions
}

type ScoredForecast struct {
	Days       []*DayForecast
	Historical *ScoredConditions
	Baseline   *ScoredConditions
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

func trimAndScore(c *Climb, f *weather.Forecast, min, max int) (*ClimbForecast, error) {
	scored := ScoredForecast{}
	result := &ClimbForecast{Climb: c, Forecast: &scored}
	if len(f.Hourly) == 0 {
		return result, nil
	}

	df := DayForecast{}
	for _, w := range f.Hourly {
		hours, _, _ := w.Time.Clock()
		if hours < min || hours > max {
			continue
		}

		s := score(c, w, calc.Rho0, 0.0, 0.0) // TODO: include historical
		day := s.Day()
		if df.Day == "" {
			df.Day = day
		} else if day != df.Day {
			scored.Days = append(scored.Days, &df)
			df = DayForecast{Day: day}
		}
		df.Conditions = append(df.Conditions, s)
		if scored.Historical == nil || s.historical > scored.Historical.historical {
			scored.Historical = s
		}
		if scored.Baseline == nil || s.baseline > scored.Baseline.baseline {
			scored.Baseline = s
		}
	}
	if df.Day != "" {
		scored.Days = append(scored.Days, &df)
	}

	// The first and last day will usually not have all the data, we pad the slices with nulls.
	hours := max - min
	pad(&scored.Days, hours)

	// Verify invariants
	if len(scored.Days) != 7 {
		return nil, fmt.Errorf("expected 7 days worth of data and got %d", len(scored.Days))
	}
	for _, d := range scored.Days {
		if len(d.Conditions) != hours {
			return nil, fmt.Errorf("expected each day to have %d hours of data but %s had only %d",
				hours, d.Day, len(d.Conditions))
		}
	}

	return result, nil
}

func pad(days *[]*DayForecast, expected int) {
	if len(*days) > 0 {
		first := (*days)[0]
		actual := len(first.Conditions) // > 0
		if actual < expected {
			padded := make([]*ScoredConditions, expected)
			for i := actual; i < expected; i++ {
				padded[i] = first.Conditions[actual-i]
			}
			first.Conditions = padded
		}
	}

	if len(*days) > 1 {
		last := (*days)[len(*days)-1]
		actual := len(last.Conditions) // > 0
		if actual < expected {
			padded := make([]*ScoredConditions, expected)
			copy(padded, last.Conditions)
			last.Conditions = padded
		}
	}
}

func score(climb *Climb, conditions *weather.Conditions, rhoH, vwH, dwH float64) *ScoredConditions {
	power := perf.CalcPowerM(500, climb.Segment.Distance, climb.Segment.AverageGrade, climb.Segment.MedianElevation)

	// TODO(kjs): use c.Map polyline for more accurate score.
	historical := wnf.Power2(
		power,
		climb.Segment.Distance,
		rhoH,
		conditions.AirDensity,
		wnf.CdaClimb,
		vwH,
		conditions.WindSpeed,
		dwH,
		conditions.WindBearing,
		climb.Segment.AverageDirection,
		climb.Segment.AverageGrade,
		wnf.Mt)

	baseline := wnf.Power(
		power,
		climb.Segment.Distance,
		climb.Segment.MedianElevation,
		conditions.AirDensity,
		wnf.CdaClimb,
		conditions.WindSpeed,
		conditions.WindBearing,
		climb.Segment.AverageDirection,
		climb.Segment.AverageGrade,
		wnf.Mt)

	return &ScoredConditions{conditions, historical, baseline}
}

func (c *ScoredConditions) Rank(s float64) int {
	mod := 1
	if s < 1.0 {
		mod = -1
	}

	rank := int(math.Abs(s-1)*100) / 2
	if rank > 5 {
		rank = 5
	}

	return mod * rank
}

func (c *ScoredConditions) Historical() string {
	return displayScore(c.historical)
}

func (c *ScoredConditions) Baseline() string {
	return displayScore(c.baseline)
}

func displayScore(s float64) string {
	return fmt.Sprintf("%.2f%%", (s-1)*100)
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
