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
