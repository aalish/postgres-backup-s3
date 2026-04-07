package dashboard

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
)

//go:embed templates/*.html static/*
var assets embed.FS

type Templates struct {
	pages   map[string]*template.Template
	Version string
}

func LoadTemplates(version string) (*Templates, error) {
	pages := make(map[string]*template.Template)
	pageNames := []string{"login", "dashboard", "backups", "settings", "logs"}

	for _, name := range pageNames {
		tmpl, err := template.ParseFS(
			assets,
			"templates/base.html",
			fmt.Sprintf("templates/%s.html", name),
		)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		pages[name] = tmpl
	}

	return &Templates{
		pages:   pages,
		Version: version,
	}, nil
}

func (t *Templates) Render(w io.Writer, name string, data any) error {
	tmpl, ok := t.pages[name]
	if !ok {
		return fmt.Errorf("template %s not found", name)
	}
	return tmpl.ExecuteTemplate(w, "base", data)
}

func StaticFS() fs.FS {
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return staticFS
}
