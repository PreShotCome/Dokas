// Package report renders the evidence PDF for a completed drill: timestamps
// per step, an assertions table, and an overall verdict. The rendered bytes
// are handed to internal/evidence, which stores and signs them.
package report

import (
	"encoding/json"
	"io"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/preshotcome/anything/internal/drill"
)

// Data is everything the renderer needs to lay out one drill's PDF.
type Data struct {
	Drill       drill.Drill
	Target      drill.Target
	Steps       []drill.Step
	Assertions  []drill.AssertionResult
	GeneratedAt time.Time
}

// Render writes an unsigned PDF report to outPath. Parent dirs are created
// as needed. Returns the absolute path that was written.
func Render(out io.Writer, d Data) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 18)
	pdf.Cell(0, 10, "Restore Drill Report")
	pdf.Ln(12)

	pdf.SetFont("Helvetica", "", 10)
	verdict := "PASSED"
	if d.Drill.Status != drill.StatusSucceeded {
		verdict = "FAILED"
	}
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 7, "Verdict: "+verdict, "", 1, "L", false, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Helvetica", "", 10)
	pairs := [][2]string{
		{"Drill ID", d.Drill.ID.String()},
		{"Target", d.Target.Name},
		{"Source", d.Target.SourceKind + ": " + d.Target.SourceURI},
		{"Status", string(d.Drill.Status)},
		{"Started", fmtTime(d.Drill.StartedAt)},
		{"Completed", fmtTime(d.Drill.CompletedAt)},
		{"Duration", duration(d.Drill.StartedAt, d.Drill.CompletedAt)},
		{"Generated", d.GeneratedAt.UTC().Format(time.RFC3339)},
	}
	for _, kv := range pairs {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(35, 6, kv[0], "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		pdf.MultiCell(0, 6, kv[1], "", "L", false)
	}

	if d.Drill.Error != nil && *d.Drill.Error != "" {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(0, 6, "Failure reason", "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.MultiCell(0, 5, *d.Drill.Error, "", "L", false)
	}

	pdf.Ln(4)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 7, "Steps", "", 1, "L", false, 0, "")
	stepsTable(pdf, d.Steps)

	pdf.Ln(4)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 7, "Assertions", "", 1, "L", false, 0, "")
	assertionsTable(pdf, d.Assertions)

	pdf.Ln(6)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.MultiCell(0, 4,
		"This report is sealed with a detached cryptographic signature over "+
			"its SHA-256 digest; verify it from the drill detail page.", "", "L", false)

	pdf.Ln(2)
	pdf.SetFont("Helvetica", "B", 8)
	pdf.CellFormat(0, 4, "Verified by Restore Drill — restoredrill.io", "", 1, "C", false, 0, "")

	return pdf.Output(out)
}

func stepsTable(pdf *fpdf.Fpdf, steps []drill.Step) {
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetFillColor(240, 240, 240)
	pdf.CellFormat(45, 6, "Name", "1", 0, "L", true, 0, "")
	pdf.CellFormat(25, 6, "Status", "1", 0, "L", true, 0, "")
	pdf.CellFormat(40, 6, "Started", "1", 0, "L", true, 0, "")
	pdf.CellFormat(40, 6, "Completed", "1", 0, "L", true, 0, "")
	pdf.CellFormat(0, 6, "Duration", "1", 1, "L", true, 0, "")

	pdf.SetFont("Helvetica", "", 9)
	if len(steps) == 0 {
		pdf.CellFormat(0, 6, "(no steps recorded)", "1", 1, "L", false, 0, "")
		return
	}
	for _, s := range steps {
		pdf.CellFormat(45, 6, string(s.Name), "1", 0, "L", false, 0, "")
		pdf.CellFormat(25, 6, string(s.Status), "1", 0, "L", false, 0, "")
		pdf.CellFormat(40, 6, fmtTime(s.StartedAt), "1", 0, "L", false, 0, "")
		pdf.CellFormat(40, 6, fmtTime(s.CompletedAt), "1", 0, "L", false, 0, "")
		pdf.CellFormat(0, 6, duration(s.StartedAt, s.CompletedAt), "1", 1, "L", false, 0, "")
	}
}

func assertionsTable(pdf *fpdf.Fpdf, ars []drill.AssertionResult) {
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetFillColor(240, 240, 240)
	pdf.CellFormat(30, 6, "Kind", "1", 0, "L", true, 0, "")
	pdf.CellFormat(70, 6, "Expected", "1", 0, "L", true, 0, "")
	pdf.CellFormat(60, 6, "Actual", "1", 0, "L", true, 0, "")
	pdf.CellFormat(0, 6, "Result", "1", 1, "L", true, 0, "")

	pdf.SetFont("Helvetica", "", 9)
	if len(ars) == 0 {
		pdf.CellFormat(0, 6, "(no assertions ran)", "1", 1, "L", false, 0, "")
		return
	}
	for _, ar := range ars {
		result := "PASS"
		if !ar.Passed {
			result = "FAIL"
		}
		pdf.CellFormat(30, 6, ar.Kind, "1", 0, "L", false, 0, "")
		pdf.CellFormat(70, 6, prettyJSON(ar.Expected), "1", 0, "L", false, 0, "")
		pdf.CellFormat(60, 6, prettyJSON(ar.Actual), "1", 0, "L", false, 0, "")
		pdf.CellFormat(0, 6, result, "1", 1, "L", false, 0, "")
	}
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

func duration(start, end *time.Time) string {
	if start == nil || end == nil {
		return "—"
	}
	return end.Sub(*start).Round(time.Millisecond).String()
}

func prettyJSON(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(b)
	}
	return string(out)
}
