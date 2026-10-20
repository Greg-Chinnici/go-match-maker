package server

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"go-match-maker/glicko"
	"go-match-maker/sim"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"num":     formatNum,
	"count":   func(n int) string { return formatNum(float64(n), 0) },
	"sub":     func(a, b float64) float64 { return a - b },
	"numPtr":  formatNumPtr,
	"place":   formatPlace,
	"signed":  formatSigned,
	"clock":   func(t time.Time) string { return t.Format("15:04:05") },
	"shortID": func(id string) string { return id[:min(8, len(id))] },
}).ParseFS(templateFS, "templates/*.html"))

const (
	scatterLimit = 1500 // players drawn in the rating vs skill scatter
	tableRows    = 30   // most recent samples in a chart's table view
)

// chartSeries is one line on a time-series chart. Key is its path in the
// Sample JSON, which the page reads; value reads it for the table view.
type chartSeries struct {
	Label string `json:"label"`
	Key   string `json:"key"`
	Slot  int    `json:"slot"` // categorical color slot, 1-based
	value func(Sample) *float64
}

type chartSpec struct {
	ID       string        `json:"id"`
	Title    string        `json:"-"`
	Subtitle string        `json:"-"`
	Digits   int           `json:"digits"`
	Min      *float64      `json:"min,omitempty"`
	Max      *float64      `json:"max,omitempty"`
	Ref      *float64      `json:"ref,omitempty"` // horizontal reference line
	RefLabel string        `json:"refLabel,omitempty"`
	Series   []chartSeries `json:"series"`
}

// Config is the JSON the page's chart code reads from data-chart.
func (c chartSpec) Config() string {
	b, _ := json.Marshal(c)
	return string(b)
}

func ptr(v float64) *float64 { return &v }

var trafficCharts = []chartSpec{
	{
		ID: "throughput", Title: "Matches per second", Subtitle: "Formed by the matchmaker and reported back",
		Series: []chartSeries{
			{Label: "Formed", Key: "created", Slot: 1, value: func(s Sample) *float64 { return ptr(float64(s.Created)) }},
			{Label: "Reported", Key: "reported", Slot: 2, value: func(s Sample) *float64 { return ptr(float64(s.Reported)) }},
		},
	},
	{
		ID: "queue", Title: "Players waiting", Subtitle: "Queued and not yet in a match",
		Series: []chartSeries{
			{Label: "Waiting", Key: "queued", Slot: 1, value: func(s Sample) *float64 { return ptr(float64(s.Queued)) }},
		},
	},
	{
		ID: "wait", Title: "Queue wait (seconds)", Subtitle: "Players who got a match, and whoever is still waiting longest",
		Digits: 1,
		Series: []chartSeries{
			{Label: "Average for matched players", Key: "avg_wait_s", Slot: 1, value: func(s Sample) *float64 { return s.AvgWaitS }},
			{Label: "Longest still waiting", Key: "oldest_wait_s", Slot: 2, value: func(s Sample) *float64 { return ptr(s.OldestWaitS) }},
		},
	},
	{
		ID: "gap", Title: "Team balance", Subtitle: "Rating gap between the strongest and weakest team in new matches",
		Series: []chartSeries{
			{Label: "Rating gap", Key: "team_gap", Slot: 1, value: func(s Sample) *float64 { return s.TeamGap }},
		},
	},
}

var modelCharts = []chartSpec{
	{
		ID: "correlation", Title: "Correlation", Subtitle: "Between rating and true skill; 1 is a perfect ranking",
		Digits: 3, Min: ptr(0), Max: ptr(1),
		Series: []chartSeries{
			{Label: "Correlation", Key: "sim.correlation", Slot: 1, value: func(s Sample) *float64 { return ptr(s.Sim.Correlation) }},
		},
	},
	{
		ID: "slope", Title: "Rating scale", Subtitle: "Slope of rating on true skill; above 1 is stretched",
		Digits: 2, Ref: ptr(1), RefLabel: "Calibrated",
		Series: []chartSeries{
			{Label: "Slope", Key: "sim.slope", Slot: 1, value: func(s Sample) *float64 { return ptr(s.Sim.Slope) }},
		},
	},
	{
		ID: "rmse", Title: "Rating error", Subtitle: "Root-mean-square gap between rating and true skill",
		Min: ptr(0),
		Series: []chartSeries{
			{Label: "RMSE", Key: "sim.rmse", Slot: 1, value: func(s Sample) *float64 { return ptr(s.Sim.RMSE) }},
		},
	},
}

func findChart(id string) (chartSpec, bool) {
	for _, c := range slices.Concat(trafficCharts, modelCharts) {
		if c.ID == id {
			return c, true
		}
	}
	return chartSpec{}, false
}

type pageData struct {
	Info          Info
	Latest        Sample
	Recent        []MatchEvent
	TrafficCharts []chartSpec
	ModelCharts   []chartSpec
}

type tableData struct {
	Headers []string
	Rows    [][]string
}

// pointsPayload is what the scatter and histogram charts draw.
type pointsPayload struct {
	Points []sim.Point   `json:"points"`
	Hist   sim.Histogram `json:"hist"`
}

func currentPoints() pointsPayload {
	points := SimStats.Points(scatterLimit)
	return pointsPayload{Points: points, Hist: sim.NewHistogram(points, 100)}
}

func registerDashboard(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, "dashboard.html", pageData{
			Info:          telemetry.Info(),
			Latest:        telemetry.Latest(),
			Recent:        telemetry.Recent(),
			TrafficCharts: trafficCharts,
			ModelCharts:   modelCharts,
		})
	})

	// HTML table views of each chart, loaded when its "Show table" opens
	mux.HandleFunc("GET /dashboard/table/{chart}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("chart")
		switch id {
		case "scatter":
			render(w, "table", scatterTable(currentPoints().Points))
		case "histogram":
			render(w, "table", histogramTable(currentPoints().Hist))
		default:
			spec, ok := findChart(id)
			if !ok {
				http.NotFound(w, r)
				return
			}
			render(w, "table", seriesTable(spec, telemetry.History()))
		}
	})

	mux.HandleFunc("GET /sim/points", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, currentPoints())
	})

	mux.HandleFunc("GET /events", streamEvents)
}

// streamEvents sends the dashboard live data as server-sent events: HTML
// fragments ("kpis", "matches") that htmx swaps in, and JSON ("history",
// "chart", "points") that the chart code draws.
func streamEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	rc := http.NewResponseController(w)

	samples, stop := telemetry.Subscribe()
	defer stop()

	send := func(event string, data []byte) bool {
		if err := writeEvent(w, event, data); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	sendJSON := func(event string, v any) bool {
		b, err := json.Marshal(v)
		return err == nil && send(event, b)
	}
	sendHTML := func(event, name string, data any) bool {
		var buf bytes.Buffer
		if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
			return false
		}
		return send(event, buf.Bytes())
	}

	if !sendJSON("history", telemetry.History()) ||
		!sendJSON("points", currentPoints()) ||
		!sendHTML("kpis", "kpis", telemetry.Latest()) ||
		!sendHTML("matches", "matches", telemetry.Recent()) {
		return
	}

	for tick := 1; ; tick++ {
		select {
		case <-r.Context().Done():
			return
		case s := <-samples:
			ok := sendJSON("chart", s) &&
				sendHTML("kpis", "kpis", s) &&
				sendHTML("matches", "matches", telemetry.Recent())
			if ok && tick%3 == 0 {
				ok = sendJSON("points", currentPoints())
			}
			if !ok {
				return
			}
		}
	}
}

// writeEvent writes one server-sent event, prefixing every line of data.
func writeEvent(w io.Writer, event string, data []byte) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "event: %s\n", event)
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		fmt.Fprintf(&buf, "data: %s\n", line)
	}
	buf.WriteString("\n")
	_, err := w.Write(buf.Bytes())
	return err
}

func render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func seriesTable(spec chartSpec, history []Sample) tableData {
	t := tableData{Headers: []string{"Time"}}
	for _, s := range spec.Series {
		t.Headers = append(t.Headers, s.Label)
	}

	start := max(0, len(history)-tableRows)
	for i := len(history) - 1; i >= start; i-- {
		sample := history[i]
		row := []string{time.UnixMilli(sample.Time).Format("15:04:05")}
		for _, s := range spec.Series {
			row = append(row, formatNumPtr(s.value(sample), spec.Digits))
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

func scatterTable(points []sim.Point) tableData {
	sorted := slices.SortedFunc(slices.Values(points), func(a, b sim.Point) int {
		return cmp.Compare(b.Skill, a.Skill)
	})
	t := tableData{Headers: []string{"True skill", "Rating", "Rating − skill"}}
	for _, p := range sorted[:min(100, len(sorted))] {
		t.Rows = append(t.Rows, []string{formatNum(p.Skill, 0), formatNum(p.Rating, 0), formatSigned(p.Rating - p.Skill)})
	}
	return t
}

func histogramTable(h sim.Histogram) tableData {
	t := tableData{Headers: []string{"Range", "Players by rating", "Players by true skill"}}
	for i, lo := range h.BinStart {
		t.Rows = append(t.Rows, []string{
			fmt.Sprintf("%s–%s", formatNum(lo, 0), formatNum(lo+100, 0)),
			strconv.Itoa(h.Ratings[i]),
			strconv.Itoa(h.Skills[i]),
		})
	}
	return t
}

// formatNum rounds to digits places with thousands separators.
func formatNum(v float64, digits int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "–"
	}
	s := strconv.FormatFloat(math.Abs(v), 'f', digits, 64)
	whole, frac, _ := strings.Cut(s, ".")

	var b strings.Builder
	if v < 0 && strings.Trim(s, "0.") != "" {
		b.WriteString("−")
	}
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if frac != "" {
		b.WriteString("." + frac)
	}
	return b.String()
}

func formatNumPtr(v *float64, digits int) string {
	if v == nil {
		return "–"
	}
	return formatNum(*v, digits)
}

func formatSigned(v float64) string {
	if math.Round(v) > 0 {
		return "+" + formatNum(v, 0)
	}
	return formatNum(v, 0)
}

func formatPlace(place int) string {
	if place == glicko.Unplaced {
		return "Unplaced"
	}
	suffix := "th"
	if place%100 < 11 || place%100 > 13 {
		switch place % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(place) + suffix
}
