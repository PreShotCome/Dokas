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
	pdf.Cell(0, 10, "Soteria Report")
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
	pdf.CellFormat(0, 4, "Verified by Soteria — soteria.io", "", 1, "C", false, 0, "")

	return pdf.Output(out)
}

// tableLineH is the height of one wrapped line of body text, in mm.
const tableLineH = 5.0

// table renders a bordered, word-wrapping table that paginates: when a row
// would cross the bottom margin a new page is started and the column header
// is repeated — so a long assertions list is never clipped or run off-page.
func table(pdf *fpdf.Fpdf, widths []float64, header []string, rows [][]string, emptyMsg string) {
	drawHeader := func() {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetFillColor(240, 240, 240)
		tableRow(pdf, widths, header, true, nil)
		pdf.SetFont("Helvetica", "", 9)
	}
	drawHeader()
	if len(rows) == 0 {
		var total float64
		for _, w := range widths {
			total += w
		}
		pdf.CellFormat(total, 6, emptyMsg, "1", 1, "L", false, 0, "")
		return
	}
	for _, row := range rows {
		tableRow(pdf, widths, row, false, drawHeader)
	}
}

// tableRow draws one row whose cells wrap to their column width. It measures
// the tallest column first and, if the row won't fit, starts a new page
// (re-drawing the header via onNewPage) before drawing — so rows are never
// split across a page boundary.
func tableRow(pdf *fpdf.Fpdf, widths []float64, cells []string, fill bool, onNewPage func()) {
	maxLines := 1
	for i, c := range cells {
		// SplitText indexes a 256-entry core-font width table by raw rune, so
		// it panics on any rune > 0xFF — measure a Latin-1-safe copy.
		if n := len(pdf.SplitText(latin1(c), widths[i]-2)); n > maxLines {
			maxLines = n
		}
	}
	rowH := float64(maxLines) * tableLineH

	lMargin, _, _, bMargin := pdf.GetMargins()
	_, pageH := pdf.GetPageSize()
	if pdf.GetY()+rowH > pageH-bMargin {
		pdf.AddPage()
		if onNewPage != nil {
			onNewPage()
		}
	}

	style := "D"
	if fill {
		style = "FD"
	}
	x, y := pdf.GetX(), pdf.GetY()
	for i, c := range cells {
		pdf.Rect(x, y, widths[i], rowH, style)
		pdf.SetXY(x+1, y+1)
		pdf.MultiCell(widths[i]-2, tableLineH, c, "", "L", false)
		x += widths[i]
	}
	pdf.SetXY(lMargin, y+rowH)
}

func stepsTable(pdf *fpdf.Fpdf, steps []drill.Step) {
	rows := make([][]string, 0, len(steps))
	for _, s := range steps {
		rows = append(rows, []string{
			string(s.Name), string(s.Status),
			fmtTime(s.StartedAt), fmtTime(s.CompletedAt),
			duration(s.StartedAt, s.CompletedAt),
		})
	}
	table(pdf, []float64{45, 25, 40, 40, 30},
		[]string{"Name", "Status", "Started", "Completed", "Duration"},
		rows, "(no steps recorded)")
}

func assertionsTable(pdf *fpdf.Fpdf, ars []drill.AssertionResult) {
	rows := make([][]string, 0, len(ars))
	for _, ar := range ars {
		result := "PASS"
		if !ar.Passed {
			result = "FAIL"
		}
		rows = append(rows, []string{ar.Kind, prettyJSON(ar.Expected), prettyJSON(ar.Actual), result})
	}
	table(pdf, []float64{30, 70, 60, 20},
		[]string{"Kind", "Expected", "Actual", "Result"},
		rows, "(no assertions ran)")
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func duration(start, end *time.Time) string {
	if start == nil || end == nil {
		return "-"
	}
	return end.Sub(*start).Round(time.Millisecond).String()
}

// latin1 replaces runes outside the core-font (cp1252) range so fpdf text
// measurement cannot panic on, e.g., a unicode character in error text.
func latin1(s string) string {
	for _, r := range s {
		if r > 0xFF {
			safe := make([]rune, 0, len(s))
			for _, r := range s {
				if r > 0xFF {
					r = '?'
				}
				safe = append(safe, r)
			}
			return string(safe)
		}
	}
	return s
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
