package server

import (
	_ "embed"
	"fmt"
	"net/http"
	"text/template"
)

//go:embed templates/setup.sh
var setupScriptTemplate string

//go:embed templates/setup.ps1
var setupPS1Template string

func (s *Server) installScriptHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	host := r.Host
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	masterURL := fmt.Sprintf("%s://%s", scheme, host)

	tmpl, err := template.New("setup.sh").Parse(setupScriptTemplate)
	if err != nil {
		http.Error(w, "failed rendering setup script", http.StatusInternalServerError)
		return
	}

	_ = tmpl.Execute(w, struct {
		MasterURL string
	}{
		MasterURL: masterURL,
	})
}

func (s *Server) installPS1Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	host := r.Host
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	masterURL := fmt.Sprintf("%s://%s", scheme, host)

	tmpl, err := template.New("setup.ps1").Parse(setupPS1Template)
	if err != nil {
		http.Error(w, "failed rendering setup ps1 script", http.StatusInternalServerError)
		return
	}

	_ = tmpl.Execute(w, struct {
		MasterURL string
	}{
		MasterURL: masterURL,
	})
}
