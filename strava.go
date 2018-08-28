package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/schollz/closestmatch"
	"github.com/strava/go.strava"
)

type Climb struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Segment Segment  `json:"segment"`
}

type Segment struct {
	Name          string  `json:"name"`
	ID            int64   `json:"id"`
	Distance      float64 `json:"distance"`
	ElevationLow  float64 `json:"elevation_low"`
	ElevationHigh float64 `json:"elevation_high"`
}

var alphanum = regexp.MustCompile("[^a-zA-Z0-9]+")

func main() {
	var token, climbsFile string
	var outputJson bool
	var climbs []Climb

	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.BoolVar(&outputJson, "json", false, "Whether to output JSON")

	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		exit(fmt.Errorf("requires args be specified"))
	}

	if token == "" {
		token = os.Getenv("STRAVA_ACCESS_TOKEN")
		if token == "" {
			exit(fmt.Errorf("must provide a Strava access token"))
		}
	}

	if climbsFile == "" {
		climbsFile = resource("climbs.json")
	}

	file, err := ioutil.ReadFile(climbsFile)
	if err != nil {
		exit(err)
	}

	err = json.Unmarshal(file, &climbs)
	if err != nil {
		exit(err)
	}

	s, err := findSegment(token, climbs, args)
	if err != nil {
		exit(err)
	}

	if outputJson {
		j, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			exit(err)
		}
		fmt.Println(string(j))
	} else {
		fmt.Printf("-d=%.2f -e=%.2f -h=%.2f\n",
			s.Distance,
			s.ElevationHigh-s.ElevationLow,
			(s.ElevationLow+s.ElevationHigh)/2)
	}
}

func findSegment(token string, climbs []Climb, args []string) (*Segment, error) {
	if len(args) == 1 {
		id, err := strconv.ParseInt(args[0], 10, 0)
		if err == nil {
			return findSegmentByID(token, climbs, id)
		}
	}

	arg := simplify(strings.Join(args, " "))


	var names []string
	namedClimbs := make(map[string]Climb)

	for _, c := range climbs {
		n := simplify(c.Name)
		namedClimbs[n] = c
		names = append(names, n)

		n = simplify(c.Segment.Name)
		namedClimbs[n] = c
		names = append(names, n)

		for _, alias := range c.Aliases {
			a := simplify(alias)
			namedClimbs[a] = c
			names = append(names, a)
		}
	}

	exact, ok := namedClimbs[arg]
	if ok {
		return &exact.Segment, nil
	}

	cm := closestmatch.New(names, []int{1, 2, 3, 4, 5})
	closest, ok := namedClimbs[cm.Closest(arg)]
	// TODO(kjs): rule out matches which aren't very close
	if ok {
		return &closest.Segment, nil
	}

	return nil, fmt.Errorf("could not find a segment matching: %s", args)
}

func findSegmentByID(token string, climbs []Climb, segmentID int64) (*Segment, error) {
	for _, c := range climbs {
		if c.Segment.ID == segmentID {
			return &c.Segment, nil
		}
	}
	return fetchSegmentByID(token, segmentID)
}

func fetchSegmentByID(token string, segmentID int64) (*Segment, error) {
	client := strava.NewClient(token)
	s, err := strava.NewSegmentsService(client).Get(segmentID).Do()
	if err != nil {
		return nil, err
	}

	return &Segment{
		ID:            segmentID,
		Name:          s.Name,
		Distance:      s.Distance,
		ElevationHigh: s.ElevationHigh,
		ElevationLow:  s.ElevationLow,
	}, nil
}

func resource(filename string) string {
	_, src, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(src), filename)
}

func simplify(name string) string {
	return strings.ToLower(alphanum.ReplaceAllString(name, ""))
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
