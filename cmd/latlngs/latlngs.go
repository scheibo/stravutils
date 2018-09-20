package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"github.com/scheibo/geo"
	. "github.com/scheibo/stravutils"
)

func main() {
	var token, climbsFile string
	var outputJson bool

	flag.StringVar(&token, "token", "", "Access Token")
	flag.StringVar(&climbsFile, "climbs", "olh", "Climbs")
	flag.BoolVar(&outputJson, "json", false, "Whether to output JSON")

	flag.Parse()

	climbs, err := GetClimbs(climbsFile)
	if err != nil {
		exit(err)
	}

	for _, climb := range climbs {
		s := climb.Segment

		lles, err := geo.DecodeZPolyline(s.Map)
		if err != nil {
			exit(err)
		}

		//d, lo, hi, tot, gr := compute(lls, s.AverageGrade)
		d, lo, hi, tot, gr := computeZ(lles)

		fmt.Printf("%s\n---\n"+
			"distance: %.5f (%.5f) = %.5f%%\n"+
			"low: %.5f (%.5f) = %.5f%%\n"+
			"high: %.5f (%.5f) = %.5f%%\n"+
			"gain: %.5f (%.5f) = %.5f%%\n"+
			"grade: %.5f (%.5f) = %.5f%%\n\n",
			climb.Name,
			s.Distance, d, diff(s.Distance, d),
			s.ElevationLow, lo, diff(s.ElevationLow, lo),
			s.ElevationHigh, hi, diff(s.ElevationHigh, hi),
			s.TotalElevationGain, tot, diff(s.TotalElevationGain, tot),
			s.AverageGrade, gr, diff(s.AverageGrade, gr))
	}
}

func diff(before, after float64) float64 {
	return (before - after) / before * 100
}

func compute(lls []geo.LatLng, gr float64) (d, lo, hi, tot, g float64) {
	ll := lls[0] // assume len(lls) > 0
	for i := 1; i < len(lls); i++ {
		d += distance(ll, lls[i], gr)
		ll = lls[i]
	}

	lo = -1.0
	hi = -1.0
	tot = gr * d
	g = gr
	return
}

func distance(p1, p2 geo.LatLng, gr float64) float64 {
	run := geo.Distance(p1, p2)
	// NOTE: Assuming even average gradient for the entire track.
	rise := gr * run
	return math.Sqrt(run*run + rise*rise)
}

func computeZ(lles []geo.LatLngEle) (d, lo, hi, tot, g float64) {
	// assume len(lls) > 0
	lo = lles[0].Ele
	hi = lo
	tot = 0.0
	lle := lles[0]
	for i := 1; i < len(lles); i++ {
		dis, gr := distanceZ(lle, lles[i])
		d += dis
		g += gr * dis

		tot += lles[i].Ele - lle.Ele
		lle = lles[i]
		lo = math.Min(lo, lle.Ele)
		hi = math.Max(hi, lle.Ele)
	}
	g /= d
	return
}

func distanceZ(p1, p2 geo.LatLngEle) (float64, float64) {
	run := geo.Distance(p1.LatLng(), p2.LatLng())
	rise := p2.Ele - p1.Ele
	return math.Sqrt(run*run + rise*rise), rise / run
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
