package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/scheibo/perf"
)

const FTP_DURATION = 60 * time.Minute
const CLIMB_TO_FLAT = 0.9
const ROAD_TO_TT = 0.87

var MIX = [...]float64{3, 2.5, 1.5}

func main() {
	var p, t, cp, ftp, ftpc, ftpf, ftptt float64
	var x string
	var dur time.Duration

	flag.Float64Var(&p, "p", 0, "power maintained")
	flag.StringVar(&x, "x", "M", "sex of the athlete")

	flag.DurationVar(&dur, "t", 0, "duration in minutes and seconds ('12m34s')")

	flag.Parse()

	if p <= 0 {
		exit(fmt.Errorf("p must be positive but was %f", p))
	}

	if dur > 0 {
		verify("t", float64(dur))
		t = float64(dur / time.Second)
		if x == "M" {
			cp = perf.CpM(t)
			ftp = perf.CpM(float64(FTP_DURATION / time.Second))

		} else {
			cp = perf.CpF(t)
			ftp = perf.CpF(float64(FTP_DURATION / time.Second))
		}

		// Scale performance to FTP duration
		ftpc = p / cp * ftp
		ftpf = ftpc * CLIMB_TO_FLAT
		ftptt = ftpf * ROAD_TO_TT

		ftp = ((MIX[0] * ftpc) + (MIX[1] * ftpf) + (MIX[2] * ftptt)) / 7

		fmt.Printf("%.1f*(FTPc: %.2f) + %.1f*(FTPf: %.2f) + %.1f*(FTPtt: %.2f) => %.2f\n",
			MIX[0], ftpc, MIX[1], ftpf, MIX[2], ftptt, ftp)
	} else {
		exit(fmt.Errorf("t must be specified and be > 0"))
	}
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


