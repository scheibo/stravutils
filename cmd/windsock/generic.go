package main

import (
	"html/template"
	"os"
	"path/filepath"

	"github.com/tdewolff/minify"
)

func renderAllRoot(m *minify.M, t *template.Template, data *RootTmpl, historical bool, dir string) error {
	path := data.CanonicalPath
	// Historical
	data.CanonicalPath = path + "historical"
	data.Historical = true

	h := filepath.Join(dir, "historical", "index.html")
	err := executeTemplateRoot(m, t, data, h)
	if err != nil {
		return err
	}

	// Baseline
	data.CanonicalPath = path + "baseline"
	data.Historical = false

	b := filepath.Join(dir, "baseline", "index.html")
	err = executeTemplateRoot(m, t, data, b)
	if err != nil {
		return err
	}

	return index(dir, historical)
}

func renderAllDayTime(m *minify.M, t *template.Template, data *DayTimeTmpl, historical bool, dir string) error {
	path := data.CanonicalPath
	// Historical
	data.CanonicalPath = path + "historical"
	data.Historical = true

	h := filepath.Join(dir, "historical", "index.html")
	err := executeTemplateDayTime(m, t, data, h)
	if err != nil {
		return err
	}

	// Baseline
	data.CanonicalPath = path + "baseline"
	data.Historical = false

	b := filepath.Join(dir, "baseline", "index.html")
	err = executeTemplateDayTime(m, t, data, b)
	if err != nil {
		return err
	}

	return index(dir, historical)
}

func renderAllClimb(m *minify.M, t *template.Template, data *ClimbTmpl, historical bool, dir string) error {
	path := data.CanonicalPath
	// Historical
	data.CanonicalPath = path + "historical"
	data.Historical = true

	h := filepath.Join(dir, "historical", "index.html")
	err := executeTemplateClimb(m, t, data, h)
	if err != nil {
		return err
	}

	// Baseline
	data.CanonicalPath = path + "baseline"
	data.Historical = false

	b := filepath.Join(dir, "baseline", "index.html")
	err = executeTemplateClimb(m, t, data, b)
	if err != nil {
		return err
	}

	err = aliases(data.Climb.Aliases, dir, historical)
	if err != nil {
		return err
	}

	return index(dir, historical)
}

func executeTemplateRoot(m *minify.M, t *template.Template, data *RootTmpl, path string) error {
	f, err := create(path)
	if err != nil {
		return err
	}

	w := m.Writer("text/html", f)
	err = t.ExecuteTemplate(w, TEMPLATE, data)
	if err != nil {
		w.Close()
		f.Close()
		return err
	}

	err = w.Close()
	if err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

func executeTemplateDayTime(m *minify.M, t *template.Template, data *DayTimeTmpl, path string) error {
	f, err := create(path)
	if err != nil {
		return err
	}

	w := m.Writer("text/html", f)
	err = t.ExecuteTemplate(w, TEMPLATE, data)
	if err != nil {
		w.Close()
		f.Close()
		return err
	}

	err = w.Close()
	if err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

func executeTemplateClimb(m *minify.M, t *template.Template, data *ClimbTmpl, path string) error {
	f, err := create(path)
	if err != nil {
		return err
	}

	w := m.Writer("text/html", f)
	err = t.ExecuteTemplate(w, TEMPLATE, data)
	if err != nil {
		w.Close()
		f.Close()
		return err
	}

	err = w.Close()
	if err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

func aliases(as []string, orig string, historical bool) error {
	dir := filepath.Dir(orig)
	base := filepath.Base(orig)

	done := make(map[string]bool)
	for _, a := range genAliases(base, as) {
		_, ok := done[a]
		if ok {
			continue
		}
		done[a] = true

		path := filepath.Join(dir, a)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			return err
		}

		err = os.Symlink(filepath.Join("..", base, "index.html"),
			filepath.Join(path, "index.html"))
		if err != nil {
			return err
		}

		h := filepath.Join(path, "historical")
		err = os.MkdirAll(h, 0755)
		if err != nil {
			return err
		}
		err = os.Symlink(filepath.Join("..", "..", base, "historical", "index.html"),
			filepath.Join(h, "index.html"))
		if err != nil {
			return err
		}

		b := filepath.Join(path, "baseline")
		err = os.MkdirAll(b, 0755)
		if err != nil {
			return err
		}
		err = os.Symlink(filepath.Join("..", "..", base, "baseline", "index.html"),
			filepath.Join(b, "index.html"))
		if err != nil {
			return err
		}
	}
	return nil
}

func genAliases(name string, aliases []string) []string {
	var gen []string
	gen = append(gen, superSlugify(name)...)
	for _, a := range aliases {
		gen = append(gen, superSlugify(a)...)
	}
	return gen
}

func index(dir string, historical bool) error {
	i := filepath.Join(dir, "index.html")
	if historical {
		return os.Symlink("historical/index.html", i)
	} else {
		return os.Symlink("baseline/index.html", i)
	}
}

func create(path string) (*os.File, error) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
}
