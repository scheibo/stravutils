package stravutils

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/scheibo/darksky"
	"github.com/scheibo/geo"
	"github.com/scheibo/weather"
)

var now = time.Now()

type Weather struct {
	ds       *darksky.Client
	cache    string
	throttle <-chan time.Time
	loc      *time.Location
	offline  bool
}

func NewWeatherClient(key, cache string, qps int, loc *time.Location, offline bool) *Weather {
	if cache == "" {
		cache = resource("cache")
	}
	return &Weather{
		ds:       darksky.NewClient(key),
		cache:    cache,
		throttle: time.Tick(time.Second / time.Duration(qps)),
		loc:      loc,
		offline:  offline,
	}
}

func (w *Weather) Historical(ll geo.LatLng, t time.Time) (*weather.Forecast, error) {
	t = time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, t.Location())
	cache := filepath.Join(
		w.cache,
		fmt.Sprintf("%s,%s", geo.Coordinate(ll.Lat), geo.Coordinate(ll.Lng)),
		fmt.Sprintf("%d.json.gz", t.Unix()))

	if _, err := os.Stat(cache); err == nil {
		return w.load(cache)
	}

	if w.offline {
		return nil, fmt.Errorf("could not find cached results: %s", cache)
	}

	path := fmt.Sprintf("%s,%s,%d", geo.Coordinate(ll.Lat), geo.Coordinate(ll.Lng), t.Unix())
	<-w.throttle
	rc, err := w.ds.GetRaw(path, darksky.Arguments{"excludes": "alerts", "units": "si"})
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var r io.Reader
	r = rc
	if t.Before(now) {
		var buf bytes.Buffer
		tee := io.TeeReader(r, &buf)

		err = w.save(cache, tee)
		if err != nil {
			return nil, err
		}

		r = &buf
	}

	return w.toTrimmedForecast(r)
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
		forecast.Hourly = append(forecast.Hourly, weather.DarkSkyToConditions(&h, d, w.loc))
	}

	return &forecast, nil
}

type HistoricalClimbAverages map[int64]HistoricalMonthlyAverages

type HistoricalMonthlyAverages struct {
	Monthly []*HistoricalHourlyAverages `json:"monthly"`
}

type HistoricalHourlyAverages struct {
	Hourly []*weather.Conditions `json:"hourly"`
}

func GetHistoricalAverages(files ...string) (HistoricalClimbAverages, error) {
	historical := make(map[int64]HistoricalMonthlyAverages)

	file := Resource("historical")
	if len(files) > 0 && files[0] != "" {
		file = Resource(files[0])
	}

	f, err := ioutil.ReadFile(file)
	if err != nil {
		return historical, err
	}

	err = json.Unmarshal(f, &historical)
	if err != nil {
		return historical, err
	}

	return historical, nil
}

func (avgs *HistoricalClimbAverages) Get(s *Segment, t time.Time, loc *time.Location) *weather.Conditions {
	t = t.In(loc)
	_, month, _ := t.Date()
	hour, _, _ := t.Clock()

	monthly, ok := (*avgs)[s.ID]
	if !ok {
		return nil
	}
	return monthly.Monthly[month-1].Hourly[hour]
}

func create(path string) (*os.File, error) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0400)
}

func resource(name string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), name)
}
