package main

import (
	"html/template"
	"io"
	"os"
	"path/filepath"

	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/css"
	"github.com/tdewolff/minify/html"
	"github.com/tdewolff/minify/js"
	"github.com/tdewolff/minify/svg"
)

func getTemplates() map[string]*template.Template {
	templates := make(map[string]*template.Template)

	layout := resource(TEMPLATE)
	script := resource("script.tmpl.html")
	templates["root"] = template.Must(template.ParseFiles(layout, resource("root.tmpl.html")))
	templates["time"] = template.Must(template.ParseFiles(layout, resource("time.tmpl.html"), script))
	templates["climb"] = template.Must(template.ParseFiles(layout, resource("climb.tmpl.html"), script))
	return templates
}

func render(templates map[string]*template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {
	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/html", html.Minify)
	m.AddFunc("text/javascript", js.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)

	err := os.RemoveAll(dir)
	if err != nil {
		return err
	}
	err = copyFile(resource("favicon.ico"), filepath.Join(dir, "favicon.ico"))
	if err != nil {
		return err
	}

	tmpl, _ := templates["root"]
	err = renderRoot(m, tmpl, historical, absoluteURL, dir, forecasts)
	if err != nil {
		return err
	}

	tmpl, _ = templates["time"]
	err = renderDayTimes(m, tmpl, historical, absoluteURL, dir, forecasts)
	if err != nil {
		return err
	}

	tmpl, _ = templates["climb"]
	err = renderClimbs(m, tmpl, historical, absoluteURL, dir, forecasts)
	if err != nil {
		return err
	}
	return nil
}

func renderRoot(m *minify.M, t *template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {
	data := RootTmpl{LayoutTmpl{AbsoluteURL: absoluteURL, Title: "Weather"}, forecasts}
	return renderAllRoot(m, t, &data, historical, dir)
}

func renderDayTimes(m *minify.M, t *template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {

	dayTimes := make(map[string]*DayTimeTmpl)

	for i := 0; i < len(forecasts); i++ {
		cf := forecasts[i]
		for j := 0; j < len(cf.Forecast.Days); j++ {
			df := cf.Forecast.Days[j]
			for k := 0; k < len(df.Conditions); k++ {
				c := df.Conditions[k]

				if c == nil {
					continue
				}

				path := filepath.Join(dir, c.DayTimeSlug())
				existing, ok := dayTimes[path]
				if !ok {
					data := DayTimeTmpl{}
					data.AbsoluteURL = absoluteURL
					data.DayTime = c.DayTime()
					data.Slug = c.DayTimeSlug()
					data.Title = "Weather - " + data.DayTime
					data.CanonicalPath = data.Slug + "/"

					days := cf.Forecast.Days
					data.Up = dayTimeUp(days, j, k)
					data.Down = dayTimeDown(days, j, k)
					data.Left = dayTimeLeft(days, j, k)
					data.Right = dayTimeRight(days, j, k)

					dayTimes[path] = &data
					existing = &data
				}

				existing.Conditions = append(existing.Conditions, &ClimbConditions{Climb: cf.Climb, Conditions: c})
			}
		}
	}

	for dir, data := range dayTimes {
		err := renderAllDayTime(m, t, data, historical, dir)
		if err != nil {
			return err
		}
	}

	return nil
}

func renderClimbs(m *minify.M, t *template.Template, historical bool, absoluteURL, dir string, forecasts []*ClimbForecast) error {
	return nil
}

func dayTimeUp(days []*DayForecast, j, k int) string {
	if k-1 < 0 {
		return dayTimeLeft(days, j, len(days[j].Conditions)-1)
	}
	return maybeDayTimeSlug(days[j].Conditions[k-1])
}

func dayTimeDown(days []*DayForecast, j, k int) string {
	if k+1 >= len(days[j].Conditions) {
		return dayTimeRight(days, j, 0)
	}
	return maybeDayTimeSlug(days[j].Conditions[k+1])
}

func dayTimeLeft(days []*DayForecast, j, k int) string {
	if j-1 < 0 {
		return ""
	}
	return maybeDayTimeSlug(days[j-1].Conditions[k])
}

func dayTimeRight(days []*DayForecast, j, k int) string {
	if j+1 >= len(days) {
		return ""
	}
	return maybeDayTimeSlug(days[j+1].Conditions[k])
}

func maybeDayTimeSlug(c *ScoredConditions) string {
	if c == nil {
		return ""
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
