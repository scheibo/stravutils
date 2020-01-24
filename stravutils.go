package stravutils

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/oauth2"

	"github.com/scheibo/geo"
	"github.com/scheibo/perf"
	"github.com/scheibo/strava"
	"github.com/scheibo/weather"
	"github.com/scheibo/wnf"
)

const MAX_PER_PAGE = 200
const CLIMB_THRESHOLD = 0.03

type Climb struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	Segment Segment  `json:"segment"`
}

type Segment struct {
	Name               string     `json:"name"`
	ID                 int64      `json:"id"`
	Distance           float64    `json:"distance"`
	AverageGrade       float64    `json:"average_grade"`
	ElevationLow       float64    `json:"elevation_low"`
	ElevationHigh      float64    `json:"elevation_high"`
	TotalElevationGain float64    `json:"total_elevation_gain"`
	MedianElevation    float64    `json:"median_elevation"`
	StartLocation      geo.LatLng `json:"start_location"`
	EndLocation        geo.LatLng `json:"end_location"`
	AverageLocation    geo.LatLng `json:"average_location,omitempty"`
	AverageDirection   float64    `json:average_direction,omitempty"`
	Map                string     `json:"map,omitempty"`
}

func GetClimbs(files ...string) ([]Climb, error) {
	var climbs []Climb

	file := Resource("climbs")
	if len(files) > 0 && files[0] != "" {
		file = Resource(files[0])
	}

	f, err := ioutil.ReadFile(file)
	if err != nil {
		return climbs, err
	}

	err = json.Unmarshal(f, &climbs)
	if err != nil {
		return climbs, err
	}

	return climbs, nil
}

func GetSegmentByID(segmentID int64, climbs []Climb, tokens ...string) (*Segment, error) {
	for _, c := range climbs {
		if c.Segment.ID == segmentID {
			return &c.Segment, nil
		}
	}

	ctx, err := GetStravaContext(tokens...)
	if err != nil {
		return nil, err
	}
	client := strava.NewAPIClient(strava.NewConfiguration())
	s, _, err := client.SegmentsApi.GetSegmentById(*ctx, segmentID)
	if err != nil {
		return nil, err
	}

	gain := float64(s.ElevationHigh) - float64(s.ElevationLow)
	gr := gain / float64(s.Distance)
	if float64(s.AverageGrade) < CLIMB_THRESHOLD {
		gain = float64(s.TotalElevationGain)
		gr = float64(s.AverageGrade)
	}

	lls, err := geo.DecodePolyline(s.Map_.Polyline)
	if err != nil {
		return nil, err
	}

	maps, err := geo.NewClient()
	if err != nil {
		return nil, err
	}

	lles, err := maps.Elevation(lls)
	if err != nil {
		return nil, err
	}

	return &Segment{
		ID:                 segmentID,
		Name:               s.Name,
		Distance:           float64(s.Distance),
		AverageGrade:       gr,
		ElevationLow:       float64(s.ElevationLow),
		ElevationHigh:      float64(s.ElevationHigh),
		TotalElevationGain: gain,
		MedianElevation:    (float64(s.ElevationHigh) + float64(s.ElevationLow)) / 2,
		StartLocation:      geo.LatLng{s.StartLatlng[0], s.StartLatlng[1]},
		EndLocation:        geo.LatLng{s.EndLatlng[0], s.EndLatlng[1]},
		AverageLocation:    geo.Average(lls),
		AverageDirection:   geo.AverageDirection(lls),
		Map:                geo.EncodeZPolyline(lles),
	}, nil
}

func GetEfforts(segmentID int64, maxPages int, tokens ...string) ([]strava.DetailedSegmentEffort, error) {
	var efforts []strava.DetailedSegmentEffort

	ctx, err := GetStravaContext(tokens...)
	if err != nil {
		return nil, err
	}
	client := strava.NewAPIClient(strava.NewConfiguration())

	for page := 1; maxPages < 1 || page <= maxPages; page++ {
		es, _, err := client.SegmentEffortsApi.GetEffortsBySegmentId(
			*ctx, int32(segmentID), map[string]interface{}{
				"perPage": int32(MAX_PER_PAGE),
				"page":    int32(page),
			})
		if err != nil {
			return nil, err
		}

		efforts = append(efforts, es...)

		// maxPages == 0 -> heuristically terminate if we get less than a full page.
		// BUG: This heuristic is *not* guaranteed to return all efforts, use
		// maxPages < 0 to ensure we exhaust all pages with one extra request.
		if len(es) == 0 || (maxPages == 0 && len(es) < MAX_PER_PAGE) {
			break
		}
	}

	return efforts, nil
}

func GetStravaContext(codes ...string) (*context.Context, error) {
	config := &oauth2.Config{
		ClientID:     os.Getenv("STRAVA_CLIENT_ID"),
		ClientSecret: os.Getenv("STRAVA_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("STRAVA_CLIENT_REDIRECT_URI"),
		Scopes:       []string{"read_all"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://www.strava.com/oauth/authorize",
			TokenURL: "https://www.strava.com/oauth/token",
		},
	}

	var token *oauth2.Token
	if len(codes) > 0 && codes[0] != "" {
		token, err := config.Exchange(context.Background(), codes[0])
		if err != nil {
			return nil, err
		}

		err = storeNewToken(token)
		if err != nil {
			return nil, err
		}
	} else {
		file := os.Getenv("STRAVA_ACCESS_TOKEN")
		if file == "" {
			return nil, fmt.Errorf("must provide a Strava access token file")
		}

		f, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(f, &token)
		if err != nil {
			return nil, err
		}
	}

	source := &notifyRefreshTokenSource{
		new: config.TokenSource(context.Background(), token),
		t:   token,
		f:   storeNewToken,
	}
	ctx := context.WithValue(context.Background(), strava.ContextOAuth2, source)
	return &ctx, nil
}

func storeNewToken(tok *oauth2.Token) error {
	file := os.Getenv("STRAVA_ACCESS_TOKEN")
	if file == "" {
		return fmt.Errorf("must provide a Strava access token file")
	}
	j, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(file, j, 0644)
}

func Resource(name string) string {
	_, src, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(src), "data", name+".json")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return name
	} else {
		return p
	}
}

func WNF(s *Segment, current, past *weather.Conditions) (baseline, historical float64, err error) {
	power := perf.CalcPowerM(500, s.Distance, s.AverageGrade, s.MedianElevation)
	return PowerWNF(power, s, current, past)
}

func PowerWNF(power float64, s *Segment, current, past *weather.Conditions) (baseline, historical float64, err error) {
	lles, err := geo.DecodeZPolyline(s.Map)
	if err != nil {
		return
	}
	lls := geo.LatLngs(lles)

	cda := wnf.CdaClimb
	if s.AverageGrade < CLIMB_THRESHOLD {
		cda = wnf.CdaTT
	}

	baseline = wnf.PowerLL(
		power,
		lls,
		s.Distance,
		s.MedianElevation,
		current.AirDensity,
		cda,
		current.WindSpeed,
		current.WindBearing,
		s.AverageGrade,
		wnf.Mt)

	if past != nil {
		historical = wnf.Power2LL(
			power,
			lls,
			s.Distance,
			past.AirDensity,
			current.AirDensity,
			cda,
			past.WindSpeed,
			current.WindSpeed,
			past.WindBearing,
			current.WindBearing,
			s.AverageGrade,
			wnf.Mt)
	}

	return
}

type notifyRefreshTokenSource struct {
	new oauth2.TokenSource
	mu  sync.Mutex
	t   *oauth2.Token
	f   func(*oauth2.Token) error
}

func (s *notifyRefreshTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.t.Valid() {
		return s.t, nil
	}
	t, err := s.new.Token()
	if err != nil {
		return nil, err
	}
	s.t = t
	return t, s.f(t)
}
