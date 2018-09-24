package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"io/ioutil"
	"encoding/json"
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
	var hist bool
	var key, tz string
	var tf TimeFlag
	var llf LatLngFlag
	var t time.Time
	var ll *geo.LatLng

	flag.BoolVar(&hist, "historical", false, "include historical average weather conditions")
	flag.StringVar(&key, "key", "", "DarkySky API Key")
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

	var c *Climb
	var extra string

	fi, _ := os.Stdin.Stat()
	if (fi.Mode()&os.ModeCharDevice) == 0 {
		bytes, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			exit(err)
		}

		err = json.Unmarshal(bytes, c)
		if err != nil {
			extra = string(bytes)
		}
	}

	if llf.LatLng != nil {
		ll = llf.LatLng
	}

	if c != nil {
		fmt.Printf("%v %s %v\n", t, loc, *c)
	} else if ll != nil {
		// TODO include extra
		fmt.Printf("%v %s %s %s\n", t, loc, ll.String(), extra)
	} else {
		exit(fmt.Errorf("latlng or climb required"))
	}
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
