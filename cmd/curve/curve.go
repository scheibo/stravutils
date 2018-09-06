package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/scheibo/calc"
)

const MR = 67.0

var DURATIONS = [...]int{5, 10, 30, 60, 180, 300, 600, 1200, 1800, 3600}

func CaM(t float64) float64 {
	return 1372.73/(1+t/20.44) + 427.21/(1+t/24994.53)
}

func CpM(t float64) float64 {
	return 422.58 + 23296.7801287949/t
}

func main() {
	var cp bool
	var mr, p1, t1, p2, t2 float64
	var dur1, dur2 time.Duration

	flag.BoolVar(&cp, "cp", false, "whether to use the CP model")
	flag.Float64Var(&mr, "mr", 67.0, "the mass of the rider in kg")

	flag.Float64Var(&p1, "p1", 0, "power maintained for t1")
	flag.Float64Var(&p2, "p2", 0, "power maintained for t2")
	flag.DurationVar(&dur1, "t1", 0, "duration in minutes and seconds ('12m34s')")
	flag.DurationVar(&dur2, "t2", 0, "duration in minutes and seconds ('12m34s')")

	flag.Parse()

	if p1 <= 0 || dur1 <= 0 {
		exit(fmt.Errorf("p1 and t1 must both specified and be > 0"))
	}

	verify("t1", float64(dur1))
	t1 = float64(dur1 / time.Second)

	if p2 > 0 && dur2 > 0 {
		exit(fmt.Errorf("p2 and t2 can't both be specified"))
	}

	scale := (p1 / mr) / (power(t1, cp) / MR)
	if p2 <= 0 && dur2 <= 0 {
		for _, t := range DURATIONS {
			p := power(float64(t), cp) * scale
			output(p, float64(t), mr)
		}
	} else if p2 > 0 {
		t2 = duration(p2, scale, cp)

		output(p2, t2, mr)
	} else {
		verify("t2", float64(dur2))
		t2 = float64(dur2 / time.Second)

		p2 = power(t2, cp) * scale
		output(p2, t2, mr)
	}
}

func output(p, t, mr float64) {
	fmt.Printf("%s: %.2f W (%.2f W/kg)\n", time.Duration(t)*time.Second, p, p/mr)
}

func power(t float64, cp bool) float64 {
	if cp {
		return CpM(t)
	} else {
		return CaM(t)
	}
}

func duration(p, scale float64, cp bool) float64 {
	// epsilon is some small value that determines when we will stop the search
	const epsilon = 1e-6
	// max is the maxmium number of iterations of the search
	const max = 100

	tl, tm, th := 0.0, 3600.0, 7200.0
	for j := 0; j < max; j++ {

		p1 := power(tm, cp) * scale
		if calc.Eqf(p1, p, epsilon) {
			break
		}

		if p1 > p {
			tl = tm
		} else {
			th = tm
		}

		tm = (th + tl) / 2.0
	}

	return tm
}

func verify(s string, x float64) {
	if x < 0 {
		exit(fmt.Errorf("%s must be non negative but was %f", s, x))
	}
}

func exit(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	flag.PrintDefaults()
	os.Exit(1)
}
