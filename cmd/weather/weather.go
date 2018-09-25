package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/araddon/dateparse"
	"github.com/scheibo/geo"
	. "github.com/scheibo/stravutils"
)

type TimeFlag struct {
	Time *time.Time
}

func (t *TimeFlag) String() string {
	return fmt.Sprintf("%s", t.Time)
}

func (t *TimeFlag) Set(v string) error {
	parsed, err := dateparse.ParseLocal(strings.TrimSpace(v))
	if err != nil {
		return err
	}
	t.Time = &parsed
	return nil
}

type LatLngFlag struct {
	LatLng *geo.LatLng
}

func (ll *LatLngFlag) String() string {
	return fmt.Sprintf("%s", ll.LatLng)
}

func (ll *LatLngFlag) Set(v string) error {
	latlng, err := geo.ParseLatLng(v)
	if err != nil {
		return err
	}

	ll.LatLng = &latlng
	return nil
}

/// MODE:
//

// COMPUTE WNF scores for climb (or if no climb, general score for latlng at time). if --historical, do historical lookup and use Time2 instead
// output: if piped, params (no historical params? calc will choke), otherwise condtions + score

func main() {
	var hist, offline bool
	var key, cache, tz string
	var qps int
	var tf TimeFlag
	var llf LatLngFlag
	var t time.Time
	var ll *geo.LatLng

	flag.BoolVar(&hist, "historical", false, "include historical average weather conditions")
	flag.StringVar(&key, "key", os.Getenv("DARKSKY_API_KEY"), "DarkySky API Key")
	flag.StringVar(&cache, "cache", "", "cache directory for historical queries")
	flag.IntVar(&qps, "qps", 100, "maximum queries per second against darksky")
	flag.BoolVar(&offline, "offline", false, "whether or not to run in offline mode")
	flag.StringVar(&tz, "tz", "America/Los_Angeles", "timezone to use")
	flag.Var(&llf, "latlng", "latitude and longitude to query weather information for")
	flag.Var(&tf, "time", "time to query weather information for")

	flag.Parse()

	if tf.Time != nil {
		t = *tf.Time
	} else {
		t = time.Now()
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		exit(err)
	}

	var climb *Climb
	var extra string

	fi, _ := os.Stdin.Stat()
	if (fi.Mode() & os.ModeCharDevice) == 0 {
		bytes, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			exit(err)
		}

		err = json.Unmarshal(bytes, climb)
		if err != nil {
			extra = string(bytes)
		}
	}

	if llf.LatLng != nil {
		ll = llf.LatLng
	}

	w := NewWeatherClient(key, cache, qps, loc, offline)
	if climb != nil {
		c := HistoricalConditions(w, ll, t, loc)

		var past *weather.Conditions
		if hist {
			avgs, err := GetHistoricalAverages()
			if err != nil {
				exit(err)
			}
			past := avgs.Get(climb, t, loc)
		}

		baseline, historical, err := WNF(climb, current, past, loc)
		if err != nil {
			exit(err)
		}

		fi, _ := os.Stdout.Stat()
		if (fi.Mode() & os.ModeCharDevice) != 0 {
			h := ""
			if hist {
				fmt.Printf("(%s => %s)\n", weatherString(past), displayScore(historical))
			}
			fmt.Printf("%s => %s\n%s", weatherString(c), displayScore(baseline), h)
		} else {
			s := climb.Segment
			fmt.Printf("-rho=%.4f -vs=%.3f -dw=%.2f -db=%.2f -d=%.2f -e=%.2f -h=%.2f\n",
				c.AirDensity, c.WindSpeed, c.WindBearing, s.AverageDirection, s.Distance, s.TotalElevationGain, s.MedianElevation)
		}
	} else if ll != nil {
		c := HistoricalConditions(w, ll, t, loc)
		// NOTE: must specify -db!
		fmt.Printf("-rho=%.4f -vs=%.3f -dw=%2.f %s\n", c.AirDensity, c.WindSpeed, c.WindBearing, extra)
	} else {
		exit(fmt.Errorf("latlng or climb required"))
	}
}

func HistoricalConditions(w *Weather, ll geo.LatLng, t time.Time, loc *time.Location) (*weather.Condtions, error) {
	f := w.Historical(ll, t)
	if len(f.Hourly) != 24 {
		return nil, fmt.Errorf("forecast is wrong size: want 24, got %d", len(f.Hourly))
	}

	t = t.In(loc)
	hour, _, _ := t.Clock()
	return f.Hourly[hour], nil
}

func displayScore(s float64) string {
	return fmt.Sprintf("%.2f%%", (s-1)*100)
}

func weatherString(c *weather.Conditions) string {
	precip := ""
	if c.PrecipProbability > 0.1 {
		precip = fmt.Sprintf("\n%s", c.Precip())
	}
	return fmt.Sprintf("%.1f°C (%.3f kg/m³)%s\n%s", c.Temperature, c.AirDensity, precip, c.Wind())
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
