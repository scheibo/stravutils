package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"path/filepath"
	"runtime"

	"github.com/scheibo/darksky"
	"github.com/scheibo/geo"
	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
)

type Provider struct {
	ds       *darksky.Client
	w        *weather.DarkSkyProvider
	cache    string
	throttle <-chan time.Time
}

func main() {
	var token, climbsFile string
	var key, cache, begin, end string
	var qps int

	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.StringVar(&key, "key", os.Getenv("DARKSKY_API_KEY"), "DarkySky API Key")
	flag.StringVar(&cache, "cache", resource("cache"), "cache directory for historical queries")
	//flag.StringVar(&begin, "begin", "2015-01-01", "YYYY-MM-DD to start from")
	//flag.StringVar(&end, "end", "2018-01-01", "YYYY-MM-DD to end at")
	flag.StringVar(&begin, "begin", "2015-10-30", "YYYY-MM-DD to start from")
	flag.StringVar(&end, "end", "2015-11-04", "YYYY-MM-DD to end at")
	flag.IntVar(&qps, "qps", 10, "maximum queries per second against darksky")

	flag.Parse()

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

	w := weather.NewClient(
		weather.Custom(&Provider{
			ds:       darksky.NewClient(key),
			w:        weather.NewDarkSkyProvider(key, loc),
			cache:    cache,
			throttle: time.Tick(time.Second / time.Duration(qps)),
		}))

	for d := t1; d.Before(t2); d = d.AddDate(0, 0, 1) {
		fmt.Printf("%v\n", d)
		for _, c := range climbs {
			fmt.Printf("%s\n", c.Name)
			f, err := w.History(c.Segment.AverageLocation, d)
			if err != nil {
				exit(err)
			}
			fmt.Printf("%s\n", f)
		}
	}
}
func (p *Provider) Current(ll geo.LatLng) (*weather.Conditions, error) {
	<-p.throttle
	return p.w.Current(ll)
}

func (p *Provider) Forecast(ll geo.LatLng) (*weather.Forecast, error) {
	<-p.throttle
	return p.w.Forecast(ll)
}

func (p *Provider) History(ll geo.LatLng, t time.Time) (*weather.Conditions, error) {
	cache := filepath.Join(
		p.cache,
		fmt.Sprintf("%s,%s", geo.Coordinate(ll.Lat), geo.Coordinate(ll.Lng)),
		fmt.Sprintf("%d.json.gz", t.Unix()))

	if _, err := os.Stat(cache); err == nil {
		return p.load(cache)
	}

	path := fmt.Sprintf("%s,%s,%d", geo.Coordinate(ll.Lat), geo.Coordinate(ll.Lng), t.Unix())
	<-p.throttle
	r, err := p.ds.GetRaw(path, weather.DarkSkyHistoryArguments)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var buf bytes.Buffer
	tee := io.TeeReader(r, &buf)

	err = p.save(cache, tee)
	if err != nil {
		return nil, err
	}

	return p.toConditions(&buf)
}

func (p *Provider) load(path string) (*weather.Conditions, error) {
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

	return p.toConditions(gz)
}

func (p *Provider) save(path string, r io.Reader) error {
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

func (p *Provider) toConditions(r io.Reader) (*weather.Conditions, error) {
	var f darksky.Forecast

	decoder := json.NewDecoder(r)
	err := decoder.Decode(&f)
	if err != nil {
		return nil, err
	}

	if len(f.Daily.Data) < 1 {
		return nil, fmt.Errorf("missing daily data")
	}
	return p.w.ToConditions(f.Currently, &f.Daily.Data[0]), nil
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
