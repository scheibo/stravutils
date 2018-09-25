package main

import (
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
)

var SLUG_REGEXP = regexp.MustCompile("[^\\p{L}\\d]+")

type ClimbForecast struct {
	Climb    *Climb
	Forecast *ScoredForecast
}

func (f *ClimbForecast) Slug() string {
	return slugify(f.Climb.Name)
}

func (f *ClimbForecast) ClimbDirection() string {
	return weather.Direction(f.Climb.Segment.AverageDirection)
}

type ScoredForecast struct {
	Current    *ScoredConditions
	Days       []*DayForecast
	historical *ScoredConditions
	baseline   *ScoredConditions
}

func (f *ScoredForecast) Best(historical bool) *ScoredConditions {
	if historical {
		return f.historical
	} else {
		return f.baseline
	}
}

type DayForecast struct {
	Day        string
	Conditions []*ScoredConditions
	dDay       string
}

type ScoredConditions struct {
	*weather.Conditions
	LocalTime  time.Time
	historical float64
	baseline   float64
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

func (c *ScoredConditions) Weather() string {
	return weatherString(c.Conditions)
}

func weatherString(c *weather.Conditions) string {
	precip := ""
	if c.PrecipProbability > 0.1 {
		precip = fmt.Sprintf("\n%s", c.Precip())
	}
	return fmt.Sprintf("%.1f°C (%.3f kg/m³)%s\n%s", c.Temperature, c.AirDensity, precip, c.Wind())
}

func (c *ScoredConditions) disambiguatedDay() string {
	return c.LocalTime.Format("Monday 2")
}

func (c *ScoredConditions) Day() string {
	return c.LocalTime.Format("Monday")
}

func (c *ScoredConditions) DayTime() string {
	return dayTime(c.LocalTime)
}

func (c *ScoredConditions) DayTimeSlug() string {
	return slugify(c.DayTime())
}

func (c *ScoredConditions) FullTime() string {
	return fullTime(c.LocalTime)
}

type LayoutTmpl struct {
	GenerationTime time.Time
	AbsoluteURL    string
	CanonicalPath  string
	Title          string
	Historical     bool
	Default        bool
}

func (t *LayoutTmpl) RootedPath(p string) string {
	u, err := url.Parse(t.AbsoluteURL)
	if err != nil {
		return p
	}
	r := "/"
	if u.Path != "" {
		r = u.Path
	}
	return filepath.Join(r, p)
}

func (t *LayoutTmpl) Path() string {
	d := filepath.Dir(t.CanonicalPath)
	if d == "." {
		d = ""
	}
	r := t.RootedPath(d)
	if r == "/" {
		return ""
	}
	return r
}

func (t *LayoutTmpl) GenTime() string {
	return t.GenerationTime.Format(time.Stamp)
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
	LocalTime  time.Time
	Conditions []*ClimbConditions
	historical *weather.Conditions
	Navigation
}

func (d *DayTimeTmpl) DayTime() string {
	return dayTime(d.LocalTime)
}

func (d *DayTimeTmpl) TimeTitle(historical bool) string {
	t := fullTime(d.LocalTime)
	if historical {
		t = fmt.Sprintf("%s\n%s", t, weatherString(d.historical))
	}
	return t
}

type ClimbConditions struct {
	Climb      *Climb
	Conditions *ScoredConditions
}

func (c *ClimbConditions) Slug() string {
	return slugify(c.Climb.Name)
}

func (c *ClimbConditions) ClimbDirection() string {
	return weather.Direction(c.Climb.Segment.AverageDirection)
}

type ClimbTmpl struct {
	LayoutTmpl
	Climb     *Climb
	Days      []string
	ShortDays []string
	Rows      []*ClimbTmplRow
	Navigation
}

func (t *ClimbTmpl) Slug() string {
	return slugify(t.Climb.Name)
}

func (t *ClimbTmpl) ClimbDirection() string {
	return weather.Direction(t.Climb.Segment.AverageDirection)
}

type ClimbTmplRow struct {
	LocalTime  time.Time
	Conditions []*ScoredConditions
	historical *weather.Conditions
}

func (c *ClimbTmplRow) Time() string {
	return c.LocalTime.Format("3PM")
}

func (c *ClimbTmplRow) TimeTitle(historical bool) string {
	t := fullTime(c.LocalTime)
	if historical {
		t = fmt.Sprintf("%s\n%s", t, weatherString(c.historical))
	}
	return t
}

func dayTime(lt time.Time) string {
	return lt.Format("Monday 3PM")
}

func fullTime(lt time.Time) string {
	return lt.Format("2006-01-02 15:04")
}

func slugify(s string) string {
	return strings.ToLower(strings.Trim(SLUG_REGEXP.ReplaceAllString(s, "-"), "-"))
}

func displayScore(s float64) string {
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
