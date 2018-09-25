package main

import (
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	. "github.com/scheibo/stravutils"
	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/css"
	"github.com/tdewolff/minify/html"
	"github.com/tdewolff/minify/js"
	"github.com/tdewolff/minify/svg"
)

const CURRENT_SLUG = "current"

func getTemplates() map[string]*template.Template {
	templates := make(map[string]*template.Template)

	layout := resource(TEMPLATE)
	script := resource("script.tmpl.html")
	templates["root"] = template.Must(template.ParseFiles(layout, resource("root.tmpl.html")))
	templates["time"] = template.Must(template.ParseFiles(layout, resource("time.tmpl.html"), script))
	templates["climb"] = template.Must(template.ParseFiles(layout, resource("climb.tmpl.html"), script))
	return templates
}

type Renderer struct {
	m           *minify.M
	historical  bool
	absoluteURL string
	dir         string
	forecasts   []*ClimbForecast
	hidden      int
	havgs       *HistoricalClimbAverages
	now         time.Time
	loc         *time.Location
}

func NewRenderer(historical bool, absoluteURL, dir string, forecasts []*ClimbForecast, hidden int, havgs *HistoricalClimbAverages, now time.Time, loc *time.Location) *Renderer {
	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/html", html.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)
	m.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)

	return &Renderer{m, historical, absoluteURL, dir, forecasts, hidden, havgs, now, loc}
}

func (r *Renderer) render(templates map[string]*template.Template) error {
	err := os.RemoveAll(r.dir)
	if err != nil {
		return err
	}
	err = copyFile(resource("favicon.ico"), filepath.Join(r.dir, "favicon.ico"))
	if err != nil {
		return err
	}

	tmpl, _ := templates["root"]
	err = r.renderRoot(tmpl)
	if err != nil {
		return err
	}

	tmpl, _ = templates["time"]
	err = r.renderDayTimes(tmpl)

	if err != nil {
		return err
	}

	tmpl, _ = templates["climb"]
	err = r.renderClimbs(tmpl)
	if err != nil {
		return err
	}
	return nil
}

func (r *Renderer) renderRoot(t *template.Template) error {
	data := RootTmpl{LayoutTmpl{GenerationTime: r.now, AbsoluteURL: r.absoluteURL, Title: "Windsock - Bay Area", Default: !r.historical}, r.forecasts[:r.hidden]}
	return renderAllRoot(r.m, t, &data, r.historical, r.dir)
}

func (r *Renderer) renderDayTimes(t *template.Template) error {
	dayTimes := make(map[string]*DayTimeTmpl)

	for i := 0; i < r.hidden; i++ {
		cf := r.forecasts[i]
		for j := 0; j < len(cf.Forecast.Days); j++ {
			df := cf.Forecast.Days[j]
			for k := 0; k < len(df.Conditions); k++ {
				c := df.Conditions[k]

				if c == nil {
					continue
				}

				slug := c.DayTimeSlug()
				if *c == *cf.Forecast.Current {
					slug = CURRENT_SLUG
				}

				path := filepath.Join(r.dir, slug)
				existing, ok := dayTimes[path]
				if !ok {
					data := DayTimeTmpl{}
					data.GenerationTime = r.now
					data.Default = !r.historical
					data.AbsoluteURL = r.absoluteURL
					data.LocalTime = c.LocalTime
					data.Title = "Windsock - Bay Area - " + data.DayTime()
					data.CanonicalPath = slug + "/"
					data.historical = r.havgs.Get(cf.Climb, c.LocalTime, r.loc)

					days := cf.Forecast.Days
					cur := cf.Forecast.Current
					data.Up = dayTimeUp(days, cur, j, k)
					data.Down = dayTimeDown(days, cur, j, k)
					data.Left = dayTimeLeft(days, cur, j, k)
					data.Right = dayTimeRight(days, cur, j, k)

					dayTimes[path] = &data
					existing = &data
				}

				existing.Conditions = append(existing.Conditions, &ClimbConditions{Climb: cf.Climb, Conditions: c})
			}
		}
	}

	for dir, data := range dayTimes {
		err := renderAllDayTime(r.m, t, data, r.historical, dir)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Renderer) renderClimbs(t *template.Template) error {
	if len(r.forecasts) == 0 {
		return nil
	}

	var names, short []string
	for _, df := range r.forecasts[0].Forecast.Days {
		names = append(names, df.Day)
		short = append(short, df.Day[:3])
	}

	for k, cf := range r.forecasts {
		days := cf.Forecast.Days

		data := ClimbTmpl{}
		data.Climb = cf.Climb

		data.GenerationTime = r.now
		data.Default = !r.historical
		data.AbsoluteURL = r.absoluteURL
		data.Title = "Windsock - Bay Area - " + cf.Climb.Name
		data.CanonicalPath = data.Slug() + "/"
		data.Days = names
		data.ShortDays = short

		hours := len(days[0].Conditions) // guaranteed to exist
		data.Rows = make([]*ClimbTmplRow, hours)
		for i := 0; i < hours; i++ {
			data.Rows[i] = &ClimbTmplRow{}
			data.Rows[i].Conditions = make([]*ScoredConditions, len(days))
			for j := 0; j < len(days); j++ {
				sc := days[j].Conditions[i]
				if sc != nil && data.Rows[i].LocalTime.IsZero() {
					data.Rows[i].LocalTime = sc.LocalTime
					data.Rows[i].historical = r.havgs.Get(cf.Climb, sc.LocalTime, r.loc)
				}
				data.Rows[i].Conditions[j] = sc
			}
		}

		if k < r.hidden {
			data.Up = climbUp(r.forecasts, k)
			data.Left = data.Up
			data.Down = climbDown(r.forecasts, k, r.hidden)
			data.Right = data.Down
		}

		err := renderAllClimb(r.m, t, &data, r.historical, filepath.Join(r.dir, data.Slug()))
		if err != nil {
			return err
		}
	}

	return nil
}

func climbUp(cf []*ClimbForecast, k int) string {
	if k-1 < 0 {
		return ""
	}
	return cf[k-1].Slug()
}

func climbDown(cf []*ClimbForecast, k, hidden int) string {
	if k+1 >= hidden {
		return ""
	}
	return cf[k+1].Slug()
}

func dayTimeUp(days []*DayForecast, cur *ScoredConditions, j, k int) string {
	if k-1 < 0 {
		return dayTimeLeft(days, cur, j, len(days[j].Conditions)-1)
	}
	return maybeDayTimeSlug(days[j].Conditions[k-1], cur)
}

func dayTimeDown(days []*DayForecast, cur *ScoredConditions, j, k int) string {
	if k+1 >= len(days[j].Conditions) {
		return dayTimeRight(days, cur, j, 0)
	}
	return maybeDayTimeSlug(days[j].Conditions[k+1], cur)
}

func dayTimeLeft(days []*DayForecast, cur *ScoredConditions, j, k int) string {
	if j-1 < 0 {
		return ""
	}
	return maybeDayTimeSlug(days[j-1].Conditions[k], cur)
}

func dayTimeRight(days []*DayForecast, cur *ScoredConditions, j, k int) string {
	if j+1 >= len(days) {
		return ""
	}
	return maybeDayTimeSlug(days[j+1].Conditions[k], cur)
}

func maybeDayTimeSlug(c *ScoredConditions, cur *ScoredConditions) string {
	if c == nil {
		return ""
	} else if *c == *cur {
		return CURRENT_SLUG
	}
	return c.DayTimeSlug()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}
