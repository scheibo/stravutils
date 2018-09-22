package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/scheibo/geo"
	. "github.com/scheibo/stravutils"
	"github.com/scheibo/weather"
)

func main() {
	var token, climbsFile string
	var key, cache, begin, end string
	var qps int
	var offline bool

	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.StringVar(&key, "key", os.Getenv("DARKSKY_API_KEY"), "DarkySky API Key")
	flag.StringVar(&cache, "cache", "", "cache directory for historical queries")
	flag.StringVar(&begin, "begin", "2015-01-01", "YYYY-MM-DD to start from")
	flag.StringVar(&end, "end", "2018-01-01", "YYYY-MM-DD to end at")
	//flag.StringVar(&begin, "begin", "2015-10-30", "YYYY-MM-DD to start from")
	//flag.StringVar(&end, "end", "2015-11-04", "YYYY-MM-DD to end at")
	flag.IntVar(&qps, "qps", 100, "maximum queries per second against darksky")
	flag.BoolVar(&offline, "offline", false, "whether or not to run in offline mode")

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

	w := NewWeatherClient(key, cache, qps, loc, offline)

	all := make(map[int64][12][24][]*weather.Conditions)
	for d := t1; d.Before(t2); d = d.AddDate(0, 0, 1) {
		for _, c := range climbs {
			f, err := w.Historical(c.Segment.AverageLocation, d)
			if err != nil {
				exit(err)
			}

			for _, h := range f.Hourly {
				t := h.Time.In(loc)
				_, month, _ := t.Date()
				hour, _, _ := t.Clock()
				month--

				v := all[c.Segment.ID]
				v[month][hour] = append(v[month][hour], h)
				all[c.Segment.ID] = v
			}
		}
	}

	avgs := make(map[int64]HistoricalMonthlyAverages)
	for _, c := range climbs {
		fs, _ := all[c.Segment.ID]
		hma := HistoricalMonthlyAverages{}
		hma.Monthly = make([]*HistoricalHourlyAverages, 12)
		for month := 0; month < 12; month++ {
			hha := HistoricalHourlyAverages{}
			hma.Monthly[month] = &hha
			hha.Hourly = make([]*weather.Conditions, 24)
			for hour := 0; hour < 24; hour++ {
				hma.Monthly[month].Hourly[hour] = average(&c, fs[month][hour])
			}
		}

		avgs[c.Segment.ID] = hma
	}

	j, err := json.MarshalIndent(avgs, "", "  ")
	if err != nil {
		exit(err)
	}
	fmt.Println(string(j))
}

func average(climb *Climb, cs []*weather.Conditions) *weather.Conditions {
	n := len(cs)
	if n == 0 {
		return nil
	}

	t0 := time.Time{}

	avg := cs[0]
	avg.Icon = ""
	avg.Time = t0
	avg.PrecipType = ""
	avg.SunriseTime = t0
	avg.SunsetTime = t0

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

	return avg
}

func normalizeBearing(b float64) float64 {
	return b + math.Ceil(-b/360)*360
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
