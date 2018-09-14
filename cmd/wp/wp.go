package main

import (
	"flag"
	"fmt"
	"io"
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

const TEMPLATE = "layout.tmpl.html"

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

	DEBUG_SATURDAY(forecasts) // TODO delete

	err = render(templates, !baseline, absoluteURL, output, forecasts)
	if err != nil {
		exit(err)
	}
}

// TODO DEBUG
func DEBUG_SATURDAY(forecasts []*ClimbForecast) {
	for _, cf := range forecasts {
		for _, df := range cf.Forecast.Days {
			for _, c := range df.Conditions {
				if c == nil {
					continue
				}
				if c != nil &&
					c.DayTime() == "Saturday 8AM" ||
					c.DayTime() == "Saturday 9AM" ||
					c.DayTime() == "Saturday 10AM" ||
					c.DayTime() == "Saturday 11AM" {
					fmt.Printf("%s: %s [%s] = %s [%s]\n",
						c.DayTime(),
						cf.Climb.Name,
						cf.ClimbDirection(),
						c.Score(true),
						c.Wind())
				}
			}
		}
	}
}

func getTemplates() map[string]*template.Template {
	templates := make(map[string]*template.Template)

	layout := resource(TEMPLATE)
	templates["root"] = template.Must(template.ParseFiles(layout, resource("root.tmpl.html")))
	templates["time"] = template.Must(template.ParseFiles(layout, resource("time.tmpl.html")))
	templates["climb"] = template.Must(template.ParseFiles(layout, resource("climb.tmpl.html")))
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

		s := score(c, w, calc.Rho(c.Segment.MedianElevation, calc.G), 0.0, 0.0) // TODO: include historical!
		day := s.Day()
		if df.Day == "" {
			df.Day = day
		} else if day != df.Day {
			ptr := df
			scored.Days = append(scored.Days, &ptr)
			df = DayForecast{Day: day}
		}
		df.Conditions = append(df.Conditions, s)
		if scored.historical == nil || s.historical < scored.historical.historical {
			scored.historical = s
		}
		if scored.baseline == nil || s.baseline < scored.baseline.baseline {
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

type LayoutTmpl struct {
	AbsoluteURL   string
	CanonicalPath string
	Title         string // "Weather" +
	Historical    bool
}

type Navigation struct {
	Left  string
	Right string
	Up    string
	Down  string
}

type RootTmpl struct {
	LayoutTmpl
	Forecasts []*ClimbForecast
}

type DayTimeTmpl struct {
	LayoutTmpl
	Slug       string
	DayTime    string
	Conditions []*ClimbConditions
	Navigation
}

type ClimbConditions struct {
	Climb      *Climb
	Conditions *ScoredConditions
}

func render(templates map[string]*template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {
	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/html", html.Minify)
	m.AddFunc("text/javascript", js.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)

	err := os.RemoveAll(dir)
	if err != nil {
		return err
	}
	err = copyFile(resource("favicon.ico"), filepath.Join(dir, "favicon.ico"))
	if err != nil {
		return err
	}

	tmpl, _ := templates["root"]
	err = renderRoot(m, tmpl, historical, absoluteURL, dir, forecasts)
	if err != nil {
		return err
	}
	tmpl, _ = templates["time"]
	err = renderDayTimes(m, tmpl, historical, absoluteURL, dir, forecasts)
	if err != nil {
		return err
	}
	// TODO
	//tmpl, _ = templates["climb"]
	//err = renderClimbs(tmpl, historical, absoluteURL, dir, forecasts)
	//if err != nil {
	//return err
	//}
	return nil
}

func renderRoot(m *minify.M, t *template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {
	data := RootTmpl{LayoutTmpl{AbsoluteURL: absoluteURL, Title: "Weather"}, forecasts}
	return renderAllRoot(m, t, &data, historical, dir)
}

func renderDayTimes(m *minify.M, t *template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {

	dayTimes := make(map[string]*DayTimeTmpl)

	for i := 0; i < len(forecasts); i++ {
		cf := forecasts[i]
		for j := 0; j < len(cf.Forecast.Days); j++ {
			df := cf.Forecast.Days[j]
			for k := 0; k < len(df.Conditions); k++ {
				c := df.Conditions[k]

				path := filepath.Join(dir, c.DayTimeSlug())
				existing, ok := dayTimes[path]
				if !ok {
					data := DayTimeTmpl{}
					data.AbsoluteURL = absoluteURL
					data.DayTime = c.DayTime()
					data.Slug = c.DayTimeSlug()
					data.Title = "Weather - " + data.DayTime
					data.CanonicalPath = data.Slug + "/"

					days := cf.Forecast.Days
					data.Up = dayTimeUp(days, j, k)
					data.Down = dayTimeDown(days, j, k)
					data.Left = dayTimeLeft(days, j, k)
					data.Right = dayTimeRight(days, j, k)

					dayTimes[path] = &data
					existing = &data
				}

				existing.Conditions = append(existing.Conditions, &ClimbConditions{Climb: cf.Climb, Conditions: c})
			}
		}
	}

	for dir, data := range dayTimes {
		err := renderAllDayTime(m, t, data, historical, dir)
		if err != nil {
			return err
		}
	}

	return nil
}

func dayTimeUp(days []*DayForecast, j, k int) string {
	if k-1 < 0 {
		return dayTimeLeft(days, j, len(days[j].Conditions)-1)
	}
	return days[j].Conditions[k-1].DayTimeSlug()
}

func dayTimeDown(days []*DayForecast, j, k int) string {
	if k+1 >= len(days[j].Conditions) {
		return dayTimeRight(days, j, 0)
	}
	return days[j].Conditions[k+1].DayTimeSlug()
}

func dayTimeLeft(days []*DayForecast, j, k int) string {
	if j-1 < 0 {
		return ""
	}
	return days[j-1].Conditions[k].DayTimeSlug()
}

func dayTimeRight(days []*DayForecast, j, k int) string {
	if j+1 >= len(days) {
		return ""
	}
	return days[j+1].Conditions[k].DayTimeSlug()
}

func renderAllRoot(m *minify.M, t *template.Template, data *RootTmpl, historical bool, dir string) error {
	path := data.CanonicalPath
	// Historical
	data.CanonicalPath = path + "historical"
	data.Historical = true

	h := filepath.Join(dir, "historical", "index.html")
	err := executeTemplateRoot(m, t, data, h)
	if err != nil {
		return err
	}

	// Baseline
	data.CanonicalPath = path + "baseline"
	data.Historical = false

	b := filepath.Join(dir, "baseline", "index.html")
	err = executeTemplateRoot(m, t, data, b)
	if err != nil {
		return err
	}

	// Index
	i := filepath.Join(dir, "index.html")
	if historical {
		return os.Symlink("historical/index.html", i)
	} else {
		return os.Symlink("baseline/index.html", i)
	}
}

func executeTemplateRoot(m *minify.M, t *template.Template, data *RootTmpl, path string) error {
	f, err := create(path)
	if err != nil {
		return err
	}

	w := m.Writer("text/html", f)
	err = t.ExecuteTemplate(w, TEMPLATE, data)
	if err != nil {
		w.Close()
		f.Close()
		return err
	}

	err = w.Close()
	if err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

func renderAllDayTime(m *minify.M, t *template.Template, data *DayTimeTmpl, historical bool, dir string) error {
	path := data.CanonicalPath
	// Historical
	data.CanonicalPath = path + "historical"
	data.Historical = true

	h := filepath.Join(dir, "historical", "index.html")
	err := executeTemplateDayTime(m, t, data, h)
	if err != nil {
		return err
	}

	// Baseline
	data.CanonicalPath = path + "baseline"
	data.Historical = false

	b := filepath.Join(dir, "baseline", "index.html")
	err = executeTemplateDayTime(m, t, data, b)
	if err != nil {
		return err
	}

	// Index
	i := filepath.Join(dir, "index.html")
	if historical {
		return os.Symlink("historical/index.html", i)
	} else {
		return os.Symlink("baseline/index.html", i)
	}
}

func executeTemplateDayTime(m *minify.M, t *template.Template, data *DayTimeTmpl, path string) error {
	f, err := create(path)
	if err != nil {
		return err
	}

	w := m.Writer("text/html", f)
	err = t.ExecuteTemplate(w, TEMPLATE, data)
	if err != nil {
		w.Close()
		f.Close()
		return err
	}

	err = w.Close()
	if err != nil {
		f.Close()
		return err
	}

	return f.Close()
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

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}

var SLUG_REGEXP = regexp.MustCompile("[^A-Za-z0-9]+")

func (c *ClimbConditions) Slug() string {
	return slugify(c.Climb.Name)
}

func (c *ClimbConditions) ClimbDirection() string {
	return weather.Direction(c.Climb.Segment.AverageDirection)
}

func (f *ClimbForecast) Slug() string {
	return slugify(f.Climb.Name)
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
	if s > 1.0 {
		mod = -1
	}

	rank := int(math.Abs(s-1)*100) / 2
	if rank > 5 {
		rank = 5
	}

	return mod * rank
}

func (c *ScoredConditions) Wind() string {
	return fmt.Sprintf("%.1f km/h %s", c.WindSpeed*msToKmh, weather.Direction(c.WindBearing))
}

func (c *ScoredConditions) Day() string {
	return c.Time.Format("Monday")
}

func (c *ScoredConditions) DayTime() string {
	return c.Time.Format("Monday 3PM")
}

func (c *ScoredConditions) DayTimeSlug() string {
	return slugify(c.DayTime())
}

func (c *ScoredConditions) FullTime() string {
	return c.Time.Format("2006-01-02 15:04")
}

func slugify(s string) string {
	return strings.ToLower(strings.Trim(SLUG_REGEXP.ReplaceAllString(s, "-"), "-"))
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
