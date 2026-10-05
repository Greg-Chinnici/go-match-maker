package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed panel.html
var panelFS embed.FS

var panelTemplates = template.Must(template.New("").Funcs(template.FuncMap{
	"num":      func(v float64, digits int) string { return strconv.FormatFloat(v, 'f', digits, 64) },
	"duration": formatDuration,
}).ParseFS(panelFS, "panel.html"))

// defaultConfig fills the panel form.
var defaultConfig = Config{Players: 500, Rate: 100, Duration: time.Minute}

type panelData struct {
	Base     string // this server's URL; the panel is rendered into another origin's page
	Defaults Config
	Status   Status
	Error    string
}

// runRequest is the JSON body for POST /run.
type runRequest struct {
	Players  int     `json:"players"`
	Rate     float64 `json:"rate"`
	Duration string  `json:"duration"` // Go duration, e.g. "2m"
}

func (r runRequest) config() (Config, error) {
	d, err := time.ParseDuration(r.Duration)
	if err != nil {
		return Config{}, errors.New("duration must look like 30s, 2m or 1h")
	}
	return Config{Players: r.Players, Rate: r.Rate, Duration: d}, nil
}

// newHandler serves the JSON control API and the HTML panel the dashboard
// embeds. Only allowedOrigins may call it from a browser.
func newHandler(runner *Runner, allowedOrigins []string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("loadgen: GET/POST/DELETE /run for JSON, or use the match maker dashboard\n"))
	})

	// JSON API
	mux.HandleFunc("GET /run", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.Status())
	})
	mux.HandleFunc("POST /run", func(w http.ResponseWriter, r *http.Request) {
		var req runRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		cfg, err := req.config()
		if err == nil {
			err = runner.Start(cfg)
		}
		switch {
		case errors.Is(err, ErrRunning):
			http.Error(w, err.Error(), http.StatusConflict)
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			writeJSON(w, http.StatusAccepted, runner.Status())
		}
	})
	mux.HandleFunc("DELETE /run", func(w http.ResponseWriter, r *http.Request) {
		runner.Stop()
		writeJSON(w, http.StatusOK, runner.Status())
	})

	// HTML panel for the dashboard (htmx)
	panel := func(r *http.Request, errMsg string) panelData {
		return panelData{Base: "http://" + r.Host, Defaults: defaultConfig, Status: runner.Status(), Error: errMsg}
	}
	mux.HandleFunc("GET /panel", func(w http.ResponseWriter, r *http.Request) {
		renderPanel(w, "panel", panel(r, ""))
	})
	mux.HandleFunc("GET /panel/status", func(w http.ResponseWriter, r *http.Request) {
		renderPanel(w, "status", panel(r, ""))
	})
	mux.HandleFunc("POST /panel/start", func(w http.ResponseWriter, r *http.Request) {
		errMsg := ""
		if err := startFromForm(runner, r); err != nil {
			errMsg = err.Error()
		}
		// errors render as part of the fragment: htmx doesn't swap 4xx responses
		renderPanel(w, "status", panel(r, errMsg))
	})
	mux.HandleFunc("POST /panel/stop", func(w http.ResponseWriter, r *http.Request) {
		runner.Stop()
		renderPanel(w, "status", panel(r, ""))
	})

	return cors(allowedOrigins, mux)
}

// formatDuration drops Go's zero units, so 1m0s reads as 1m.
func formatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func startFromForm(runner *Runner, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errors.New("invalid form")
	}
	players, err := strconv.Atoi(r.PostForm.Get("players"))
	if err != nil {
		return errors.New("players must be a whole number")
	}
	rate, err := strconv.ParseFloat(r.PostForm.Get("rate"), 64)
	if err != nil {
		return errors.New("rate must be a number")
	}
	cfg, err := runRequest{Players: players, Rate: rate, Duration: r.PostForm.Get("duration")}.config()
	if err != nil {
		return err
	}
	return runner.Start(cfg)
}

// htmxHeaders are the request headers htmx sends, which make every panel
// request a CORS preflighted one.
const htmxHeaders = "Content-Type, HX-Request, HX-Current-URL, HX-Target, HX-Trigger, HX-Trigger-Name"

// htmxResponseHeaders are the response headers htmx reads. Exposing them
// stops the browser logging a warning for each one on every panel request.
const htmxResponseHeaders = "HX-Location, HX-Push, HX-Push-Url, HX-Redirect, HX-Refresh, HX-Replace-Url, HX-Reswap, HX-Retarget, HX-Reselect, HX-Trigger, HX-Trigger-After-Settle, HX-Trigger-After-Swap"

// cors lets allowed origins call the API from a browser. Requests from any
// other origin are refused outright when they could change state, because a
// plain HTML form can POST cross-origin without a preflight. Requests with no
// Origin (curl, scripts) are allowed.
func cors(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		ok := origin != "" && slices.Contains(allowed, origin)

		if ok {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", htmxResponseHeaders)
			h.Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			if !ok {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
			h.Set("Access-Control-Allow-Headers", htmxHeaders)
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if origin != "" && !ok && r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func renderPanel(w http.ResponseWriter, name string, data panelData) {
	var buf bytes.Buffer
	if err := panelTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
