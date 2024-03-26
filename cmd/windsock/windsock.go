package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
)

const msToKmh = 3600.0 / 1000.0

const minHour = 6
const maxHour = 18

func main() {
	var segmentID int64
	var output, key, climbsFile, hiddenFile, absoluteURL string
	var historical bool
	var min, max int

	flag.Int64Var(&segmentID, "segmentID", 0, "Render a specific segment's climb page to the current directory and then exit.")
	flag.BoolVar(&historical, "historical", false, "Default to historical instead of baseline")
	flag.StringVar(&absoluteURL, "absoluteURL", "https://bayarea.climberrankings.com/climbs/windsock", "Absolute root URL of the site")
	flag.StringVar(&output, "output", "site", "Output directory")
	flag.StringVar(&key, "key", "", "DarkySky API Key")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.StringVar(&hiddenFile, "hidden", "", "Bonus hidden segments to include in the output")
	flag.IntVar(&min, "min", 6, "Minimum hour [0-23] to include in forecasts")
	flag.IntVar(&max, "max", 18, "Maximum hour [0-23] to include in forecasts")

	flag.Parse()

	genTime := time.Now()

	if min < 0 || max > 23 || min >= max {
		exit(fmt.Errorf("min and max must be in the range [0-23] with min < max but got min=%d max=%d", min, max))
	}

	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		exit(err)
	}

	climbs, err := GetClimbs(climbsFile)
	if err != nil {
		exit(err)
	}

	templates := getTemplates()
	w := weather.NewClient(weather.DarkSky(key), weather.TimeZone(loc))

	if segmentID != 0 {
		s, err := GetSegmentByID(segmentID, climbs)
		if err != nil {
			exit(err)
		}
		c := Climb{Name: s.Name, Segment: *s}
		cf, err := getClimbForecast(&c, w, nil /* havgs */, min, max, loc)
		if err != nil {
			exit(err)
		}
		forecasts := []*ClimbForecast{cf}
		err = NewRenderer(
			false, /* historical */
			absoluteURL,
			"", /* output */
			forecasts,
			0,   /* hidden */
			nil, /* havgs */
			genTime,
			loc).renderSegment(templates)
		if err != nil {
			exit(err)
		}
		return
	}

	hidden := len(climbs)
	if hiddenFile != "" {
		hs, err := GetClimbs(hiddenFile)
		if err != nil {
			exit(err)
		}

		for _, h := range hs {
			climbs = append(climbs, h)
		}
	}

	// NOTE: we expect all days to be present and will segfault if there are any are null.
	havgs, err := GetHistoricalAverages()
	if err != nil {
		exit(err)
	}

	var forecasts []*ClimbForecast
	for _, climb := range climbs {
		c := climb
		cf, err := getClimbForecast(&c, w, &havgs, min, max, loc)
		if err != nil {
			exit(err)
		}
		forecasts = append(forecasts, cf)
	}

	err = NewRenderer(historical, absoluteURL, output, forecasts, hidden, &havgs, genTime, loc).render(templates)
	if err != nil {
		exit(err)
	}
}

func getClimbForecast(c *Climb, w *weather.Client, h *HistoricalClimbAverages, min, max int, loc *time.Location) (*ClimbForecast, error) {
	const maxAttempts = 10                     // Maximum number of retry attempts
	const baseBackoff = 100 * time.Millisecond // Initial backoff time
	const maxBackoff = 5 * time.Second         // Maximum backoff time
	const jitterFactor = 0.5                   // Jitter factor

	var f *weather.Forecast
	var err error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Exponential backoff with jitter
    backoff := time.Duration(float64(baseBackoff) * float64(uint(1) << uint(attempt)) * (1 + (rand.Float64()-0.5)*jitterFactor))

		if backoff > maxBackoff {
			backoff = maxBackoff
		}

		// Wait for backoff duration
		time.Sleep(backoff)

		// Attempt to fetch forecast
		f, err = w.Forecast(c.Segment.AverageLocation)
		if err == nil {
			break // Success, exit retry loop
		}
	}

	if err != nil {
		return nil, err
	}

	cf, err := trimAndScore(h, c, f, min, max, loc)
	if err != nil {
		return nil, err
	}
	return cf, nil
}

func trimAndScore(h *HistoricalClimbAverages, c *Climb, f *weather.Forecast, min, max int, loc *time.Location) (*ClimbForecast, error) {
	scored := ScoredForecast{}
	result := &ClimbForecast{Climb: c, Forecast: &scored}
	if len(f.Hourly) == 0 {
		return result, nil
	}

	var past *weather.Conditions
	if h != nil {
		past = h.Get(&c.Segment, f.Hourly[0].Time, loc)
	}

	current, err := score(c, f.Hourly[0], past, loc)
	if err != nil {
		return nil, err
	}
	scored.Current = current

	df := DayForecast{}
	for i, w := range f.Hourly {

		hours, _, _ := w.Time.In(loc).Clock()
		if hours < min || hours > max {
			continue
		}

		if h != nil {
			past = h.Get(&c.Segment, w.Time, loc)
		}

		s, err := score(c, w, past, loc)
		if err != nil {
			return nil, err
		}
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
		// Don't consider the current hour for being the best score
		if i == 0 {
			continue
		}
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

func score(climb *Climb, current *weather.Conditions, past *weather.Conditions, loc *time.Location) (*ScoredConditions, error) {
	baseline, historical, err := WNF(&climb.Segment, current, past)
	if err != nil {
		return nil, err
	}
	return &ScoredConditions{current, current.Time.In(loc), historical, baseline}, nil
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
