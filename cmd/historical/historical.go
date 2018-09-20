package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"path/filepath"
	"runtime"

	"github.com/scheibo/darksky"
	"github.com/scheibo/geo"
	"github.com/scheibo/perf"
	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
	"github.com/scheibo/wnf"
)

type Weather struct {
	ds       *darksky.Client
	cache    string
	throttle <-chan time.Time
	min      int
	max      int
	loc      *time.Location
}

func main() {
	var token, climbsFile string
	var key, cache, begin, end string
	var min, max int
	var qps int

	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.StringVar(&key, "key", os.Getenv("DARKSKY_API_KEY"), "DarkySky API Key")
	flag.StringVar(&cache, "cache", resource("cache"), "cache directory for historical queries")
	//flag.StringVar(&begin, "begin", "2015-01-01", "YYYY-MM-DD to start from")
	//flag.StringVar(&end, "end", "2018-01-01", "YYYY-MM-DD to end at")
	flag.StringVar(&begin, "begin", "2015-10-30", "YYYY-MM-DD to start from")
	flag.StringVar(&end, "end", "2015-11-04", "YYYY-MM-DD to end at")
	flag.IntVar(&min, "min", 6, "Minimum hour [0-23] to include in forecasts")
	flag.IntVar(&max, "max", 18, "Maximum hour [0-23] to include in forecasts")
	flag.IntVar(&qps, "qps", 100, "maximum queries per second against darksky")

	flag.Parse()

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

	f := "2006-01-02 15:04"
	t1, err := time.ParseInLocation(f, begin+" 12:00", loc)
	if err != nil {
		exit(err)
	}

	t2, err := time.ParseInLocation(f, end+" 12:00", loc)
	if err != nil {
		exit(err)
	}

	w := Weather{
		ds:       darksky.NewClient(key),
		cache:    cache,
		throttle: time.Tick(time.Second / time.Duration(qps)),
		min:      min,
		max:      max,
		loc:      loc,
	}

	all := make(map[int64][]*weather.Conditions)
	for d := t1; d.Before(t2); d = d.AddDate(0, 0, 1) {
		for _, c := range climbs {
			f, err := w.Historical(c.Segment.AverageLocation, d)
			if err != nil {
				exit(err)
			}
			all[c.Segment.ID] = append(all[c.Segment.ID], f.Hourly...)
		}
	}

	for _, c := range climbs {
		fs, _ := all[c.Segment.ID]
		f, s := average(&c, fs)
		fmt.Printf("%s (%v -> %v):%s\nSCORE: a=%.5f b=%.5f\n\n", c.Name, t1, t2, f, s, score(&c, f))
	}
}

// TODO also compute VARIANCE - handle vector subtraction!
func average(climb *Climb, cs []*weather.Conditions) (*weather.Conditions, float64) {
	n := len(cs)
	if n == 0 {
		return &weather.Conditions{}, 0
	}

	t0 := time.Time{}

	avg := cs[0]
	avg.Icon = ""
	avg.Time = t0
	avg.PrecipType = ""
	avg.SunriseTime = t0
	avg.SunsetTime = t0
	s := score(climb, cs[0])

	var nsws, ewws, nswg, ewwg, wb float64

	for i := 1; i < n; i++ {
		c := cs[i]
		avg.Temperature += c.Temperature
		avg.Humidity += c.Humidity
		avg.ApparentTemperature += c.ApparentTemperature
		avg.PrecipProbability += c.PrecipProbability
		avg.PrecipIntensity += c.PrecipIntensity
		avg.AirPressure += c.AirPressure
		avg.AirDensity += c.AirDensity
		avg.CloudCover += c.CloudCover
		avg.UVIndex += c.UVIndex

		wb = c.WindBearing * geo.DEGREES_TO_RADIANS
		ewws += c.WindSpeed * math.Sin(wb)
		nsws += c.WindSpeed * math.Cos(wb)
		ewwg += c.WindGust * math.Sin(wb)
		nswg += c.WindGust * math.Cos(wb)

		s += score(climb, c)
	}

	f := float64(n)
	avg.Temperature /= f
	avg.Humidity /= f
	avg.ApparentTemperature /= f
	avg.PrecipProbability /= f
	avg.PrecipIntensity /= f
	avg.AirPressure /= f
	avg.AirDensity /= f
	avg.CloudCover /= f
	avg.UVIndex /= n

	ewws /= f
	nsws /= f
	ewwg /= f
	nswg /= f

	avg.WindSpeed = math.Sqrt(nsws*nsws + ewws*ewws)
	avg.WindGust = math.Sqrt(nswg*nswg + ewwg*ewwg)
	wb = math.Atan2(ewws, nsws)
	if nsws < 0 {
		wb += math.Pi
	}
	avg.WindBearing = normalizeBearing(wb * geo.RADIANS_TO_DEGREES)

	return avg, s / f
}

func score(climb *Climb, conditions *weather.Conditions) float64 {
	power := perf.CalcPowerM(500, climb.Segment.Distance, climb.Segment.AverageGrade, climb.Segment.MedianElevation)

	// TODO(kjs): use c.Map polyline for more accurate score.
	return wnf.Power(
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
}

func normalizeBearing(b float64) float64 {
	return b + math.Ceil(-b/360)*360
}

func (w *Weather) Historical(ll geo.LatLng, t time.Time) (*weather.Forecast, error) {
	cache := filepath.Join(
		w.cache,
		fmt.Sprintf("%s,%s", geo.Coordinate(ll.Lat), geo.Coordinate(ll.Lng)),
		fmt.Sprintf("%d.json.gz", t.Unix()))

	if _, err := os.Stat(cache); err == nil {
		return w.load(cache)
	}

	path := fmt.Sprintf("%s,%s,%d", geo.Coordinate(ll.Lat), geo.Coordinate(ll.Lng), t.Unix())
	<-w.throttle
	r, err := w.ds.GetRaw(path, darksky.Arguments{"excludes": "alerts,flags", "units": "si"})
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var buf bytes.Buffer
	tee := io.TeeReader(r, &buf)

	err = w.save(cache, tee)
	if err != nil {
		return nil, err
	}

	return w.toTrimmedForecast(&buf)
}

func (w *Weather) load(path string) (*weather.Forecast, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	return w.toTrimmedForecast(gz)
}

func (w *Weather) save(path string, r io.Reader) error {
	file, err := create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	if err != nil {
		return err
	}
	defer gz.Close()

	_, err = io.Copy(gz, r)
	return err
}

func (w *Weather) toTrimmedForecast(r io.Reader) (*weather.Forecast, error) {
	var f darksky.Forecast

	decoder := json.NewDecoder(r)
	err := decoder.Decode(&f)
	if err != nil {
		return nil, err
	}

	// Should be only a single daily data point for time machine requests.
	if len(f.Daily.Data) < 1 {
		return nil, fmt.Errorf("missing daily data")
	}
	d := &f.Daily.Data[0]

	forecast := weather.Forecast{}
	for _, h := range f.Hourly.Data {
		hours, _, _ := h.Time.In(w.loc).Clock()
		if hours < w.min || hours > w.max {
			continue
		}

		forecast.Hourly = append(forecast.Hourly, weather.DarkSkyToConditions(&h, d, w.loc))
	}

	return &forecast, nil
}

func resource(name string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), name)
}

func create(path string) (*os.File, error) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0400)
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
