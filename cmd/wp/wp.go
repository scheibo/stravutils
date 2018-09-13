package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

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
	historical *ScoredConditions
	baseline   *ScoredConditions
}

type ClimbForecast struct {
	Climb    *Climb
	Forecast *ScoredForecast
}

func main() {
	var output, key, climbsFile, absoluteURL string
	var baseline bool
	var min, max int

	flag.BoolVar(&baseline, "baseline", false, "Default to baseline instead of historical")
	flag.StringVar(&absoluteURL, "absoluteURL", "https://wp.scheibo.com", "Absolute root URL of the site")
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

	templates := getTemplates()
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

	err = render(templates, !baseline, absoluteURL, output, forecasts)
	if err != nil {
		exit(err)
	}
}

func getTemplates() map[string]*template.Template {
	templates := make(map[string]*template.Template)

	layout := resource("layout.tmpl.html")
	templates["root"] = template.Must(compileTemplates(layout, resource("root.tmpl.html")))
	templates["climb"] = template.Must(compileTemplates(layout, resource("climb.tmpl.html")))
	templates["time"] = template.Must(compileTemplates(layout, resource("time.tmpl.html")))
	return templates
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

		s := score(c, w, calc.Rho0, 0.0, 0.0) // TODO: include historical!
		day := s.Day()
		if df.Day == "" {
			df.Day = day
		} else if day != df.Day {
			scored.Days = append(scored.Days, &df)
			df = DayForecast{Day: day}
		}
		df.Conditions = append(df.Conditions, s)
		if scored.historical == nil || s.historical > scored.historical.historical {
			scored.historical = s
		}
		if scored.baseline == nil || s.baseline > scored.baseline.baseline {
			scored.baseline = s
		}
	}
	if df.Day != "" {
		scored.Days = append(scored.Days, &df)
	}

	// The first and last day will usually not have all the data, we pad the slices with nulls.
	hours := max - min + 1
	pad(&scored.Days, hours)

	// Verify invariants
	if len(scored.Days) != 7 {
		return nil, fmt.Errorf("expected 7 days worth of data and got %d", len(scored.Days))
	}
	for _, d := range scored.Days {
		if len(d.Conditions) != hours {
			return nil, fmt.Errorf("expected each day to have %d hours of data but %s had %d",
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
		conditions.WindBearing, // TODO make sure bearing is correct
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
		conditions.WindBearing, // TODO ditto
		climb.Segment.AverageDirection,
		climb.Segment.AverageGrade,
		wnf.Mt)

	return &ScoredConditions{conditions, historical, baseline}
}

type LayoutTmpl struct {
	AbsoluteURL   string
	CanonicalPath string
	Title         string // "Weather" +
	Historical    bool
}

type RootTmpl struct {
	LayoutTmpl
	Forecasts []*ClimbForecast
}

//type ClimbTmpl struct {
//LayoutTmpl
//// TODO
//}

//type DayTimeTmpl {
//LayoutTmpl
//DayTimeConditions
//}

//type DayTimeConditions struct {
//DayTime string
//Climbs *ClimbConditions
//}

//type ClimbConditions {
//Climb    *Climb
//Conditions *ScoredConditions
//}

func render(templates map[string]*template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {
	err := os.RemoveAll(dir)
	if err != nil {
		return err
	}

	tmpl, _ := templates["root"]
	err = renderRoot(tmpl, historical, absoluteURL, dir, forecasts)
	if err != nil {
		return err
	}
	// TODO
	// renderDayTimes(dir, forecasts)
	// renderClimbs(dir, forecasts)
	return nil
}

const TEMPLATE = "layout.tmpl.html"

func renderRoot(t *template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {
	data := RootTmpl{LayoutTmpl{AbsoluteURL: absoluteURL, Title: "Weather"}, forecasts}

	// Historical
	data.CanonicalPath = "historical"
	data.Historical = true

	h := filepath.Join(dir, "historical", "index.html")
	f, err := create(h)
	if err != nil {
		return err
	}

	err = t.ExecuteTemplate(f, TEMPLATE, data)
	f.Close()
	if err != nil {
		return err
	}

	// Baseline
	data.CanonicalPath = "baseline"
	data.Historical = false

	b := filepath.Join(dir, "baseline", "index.html")
	f, err = create(b)
	if err != nil {
		return err
	}
	err = t.ExecuteTemplate(f, TEMPLATE, data)
	f.Close()
	if err != nil {
		return err
	}

	// Index
	i := filepath.Join(dir, "index.html")
	if historical {
		return os.Symlink(h, i)
	} else {
		return os.Symlink(b, i)
	}
}

func create(path string) (*os.File, error) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
}

func resource(name string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), name)
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

var SLUG_REGEXP = regexp.MustCompile("[^A-Za-z0-9]+")

func (f *ClimbForecast) Slug() string {
	return strings.ToLower(strings.Trim(SLUG_REGEXP.ReplaceAllString(f.Climb.Name, "-"), "-"))
}

func (f *ClimbForecast) ClimbDirection() string {
	return weather.Direction(f.Climb.Segment.AverageDirection)
}

func (f *ClimbForecast) Current() *ScoredConditions {
	return f.Forecast.Days[0].Conditions[0]
}

func (f *ScoredForecast) Best(historical bool) *ScoredConditions {
	if historical {
		return f.historical
	} else {
		return f.baseline
	}
}

func (c *ScoredConditions) Score(historical bool) string {
	if historical {
		return displayScore(c.historical)
	} else {
		return displayScore(c.baseline)
	}
}

func (c *ScoredConditions) Rank(historical bool) int {
	if historical {
		return rank(c.historical)
	} else {
		return rank(c.baseline)
	}
}

func displayScore(s float64) string {
	return fmt.Sprintf("%.2f%%", (s-1)*100)
}

func rank(s float64) int {
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

func (c *ScoredConditions) Wind() string {
	return fmt.Sprintf("%.1f km/h %s", c.WindSpeed * msToKmh, weather.Direction(c.WindBearing))
}

func (c *ScoredConditions) Day() string {
	return c.Time.Format("Monday")
}

func (c *ScoredConditions) DayTime() string {
	return c.Time.Format("Monday 3PM")
}

func (c *ScoredConditions) FullTime() string {
	return c.Time.Format("2006-01-02 15:04")
}
