package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/scheibo/fuzzy"
	. "github.com/scheibo/stravutils"
)

const THRESHOLD = 0.6

var alphanum = regexp.MustCompile("[^a-zA-Z0-9]+")

func main() {
	var token, climbsFile string
	var outputJson bool

	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "", "Climbs")
	flag.BoolVar(&outputJson, "json", false, "Whether to output JSON")

	flag.Parse()
	args := flag.Args()

	climbs, err := GetClimbs(climbsFile)
	if err != nil {
		exit(err)
	}

	s, err := findSegment(token, climbs, args)
	if err != nil {
		exit(err)
	}

	fi, _ := os.Stdout.Stat()
	if outputJson || (fi.Mode()&os.ModeCharDevice) != 0 {
		j, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			exit(err)
		}
		fmt.Println(string(j))
	} else {
		fmt.Printf("-d=%.2f -e=%.2f -h=%.2f\n",
			s.Distance, s.TotalElevationGain, s.MedianElevation)
	}
}

func findSegment(token string, climbs []Climb, args []string) (*Segment, error) {
	argc := len(args)
	if argc == 1 {
		id, err := strconv.ParseInt(args[0], 10, 0)
		if err == nil {
			return GetSegmentByID(id, climbs, token)
		}
	}

	var names []string
	namedClimbs := make(map[string]Climb)

	for _, c := range climbs {
		n := simplify(c.Name, argc > 0)
		namedClimbs[n] = c
		names = append(names, n)

		n = simplify(c.Segment.Name, argc > 0)
		namedClimbs[n] = c
		names = append(names, n)

		for _, alias := range c.Aliases {
			a := simplify(alias, argc > 0)
			namedClimbs[a] = c
			names = append(names, a)
		}
	}

	if argc == 0 {
		m, err := fuzzy.FzfMatch(names)
		if err != nil {
			return nil, fmt.Errorf("could not find a segment: %s", err)
		}
		c, ok := namedClimbs[m]
		if !ok {
			return nil, fmt.Errorf("could not find a segment matching: %s", m)
		}
		return &c.Segment, nil
	}

	arg := simplify(strings.Join(args, " "), true)
	exact, ok := namedClimbs[arg]
	if ok {
		return &exact.Segment, nil
	}

	m, t := fuzzy.Match(arg, names)
	if t >= THRESHOLD {
		closest := namedClimbs[m]
		return &closest.Segment, nil
	}

	return nil, fmt.Errorf("could not find a segment matching: %s", args)
}

func simplify(name string, a bool) string {
	if a {
		return strings.ToLower(alphanum.ReplaceAllString(name, ""))
	} else {
		return name
	}
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
