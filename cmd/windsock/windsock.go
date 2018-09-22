package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/scheibo/perf"
	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
	"github.com/scheibo/wnf"
)

const msToKmh = 3600.0 / 1000.0

const TEMPLATE = "layout.tmpl.html"

const minHour = 6
const maxHour = 18

func main() {
	var output, key, climbsFile, absoluteURL string
	var baseline bool
	var min, max int

	flag.BoolVar(&baseline, "baseline", false, "Default to baseline instead of historical")
	flag.StringVar(&absoluteURL, "absoluteURL", "https://wp.scheibo.com", "Absolute root URL of the site")
	flag.StringVar(&output, "output", "site", "Output directory")
	flag.StringVar(&key, "key", "", "DarkySky API Key")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.IntVar(&min, "min", 6, "Minimum hour [0-23] to include in forecasts")
	flag.IntVar(&max, "max", 18, "Maximum hour [0-23] to include in forecasts")

	flag.Parse()

	genTime := time.Now()

	if min < 0 || max > 23 || min >= max {
		exit(fmt.Errorf("min and max must be in the range [0-23] with min < max but got min=%d max=%d", min, max))
	}

	climbs, err := GetClimbs(climbsFile)
	if err != nil {
		exit(err)
	}

	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		exit(err)
	}

	// NOTE: we expect all days to be present and will segfault if there are any are null.
	historical, err := GetHistoricalAverages()
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
		cf, err := trimAndScore(&historical, &c, f, min, max, loc)
		if err != nil {
			exit(err)
		}
		forecasts = append(forecasts, cf)
	}

	err = render(templates, !baseline, absoluteURL, output, forecasts, genTime)
	if err != nil {
		exit(err)
	}
}

func trimAndScore(h *HistoricalClimbAverages, c *Climb, f *weather.Forecast, min, max int, loc *time.Location) (*ClimbForecast, error) {
	scored := ScoredForecast{}
	result := &ClimbForecast{Climb: c, Forecast: &scored}
	if len(f.Hourly) == 0 {
		return result, nil
	}

	scored.Current = score(c, f.Hourly[0], h.Get(c, f.Hourly[0].Time, loc), loc)

	df := DayForecast{}
	for _, w := range f.Hourly {

		hours, _, _ := w.Time.In(loc).Clock()
		if hours < min || hours > max {
			continue
		}

		s := score(c, w, h.Get(c, w.Time, loc), loc)
		dDay := s.disambiguatedDay()
		if df.dDay == "" {
			df.Day = s.Day()
			df.dDay = dDay
		} else if dDay != df.dDay {
			ptr := df
			scored.Days = append(scored.Days, &ptr)
			df = DayForecast{Day: s.Day(), dDay: dDay}
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
	if len(scored.Days) > 8 || len(scored.Days) < 7 {
		return nil, fmt.Errorf("expected 8 (/7) days worth of data and got %d", len(scored.Days))
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
			for i := 0; i < actual; i++ {
				padded[expected-actual+i] = first.Conditions[i]
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

func score(climb *Climb, current *weather.Conditions, past *weather.Conditions, loc *time.Location) *ScoredConditions {
	power := perf.CalcPowerM(500, climb.Segment.Distance, climb.Segment.AverageGrade, climb.Segment.MedianElevation)

	// TODO(kjs): use c.Map polyline for more accurate score.
	historical := wnf.Power2(
		power,
		climb.Segment.Distance,
		past.AirDensity,
		current.AirDensity,
		wnf.CdaClimb,
		past.WindSpeed,
		current.WindSpeed,
		past.WindBearing,
		current.WindBearing,
		climb.Segment.AverageDirection,
		climb.Segment.AverageGrade,
		wnf.Mt)

	baseline := wnf.Power(
		power,
		climb.Segment.Distance,
		climb.Segment.MedianElevation,
		current.AirDensity,
		wnf.CdaClimb,
		current.WindSpeed,
		current.WindBearing,
		climb.Segment.AverageDirection,
		climb.Segment.AverageGrade,
		wnf.Mt)

	return &ScoredConditions{current, current.Time.In(loc), historical, baseline}
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
