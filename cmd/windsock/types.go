package main

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
)

var SLUG_REGEXP = regexp.MustCompile("[^A-Za-z0-9]+")

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
	return c.LocalTime.Format("Monday 3PM")
}

func (c *ScoredConditions) DayTimeSlug() string {
	return slugify(c.DayTime())
}

func (c *ScoredConditions) FullTime() string {
	return c.LocalTime.Format("2006-01-02 15:04")
}

type LayoutTmpl struct {
	GenerationTime time.Time
	AbsoluteURL    string
	CanonicalPath  string
	Title          string
	Historical     bool
	Default        bool
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
	DayTime    string
	FullTime   string
	Conditions []*ClimbConditions
	Navigation
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
	Time       string
	FullTime   string
	Conditions []*ScoredConditions
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

	rank := int(math.Abs(s-1)*100) / 3 // 15+
	if rank > 5 {
		rank = 5
	}

	return mod * rank
}
