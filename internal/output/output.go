package output

import (
	"fmt"
	"io"
	"strings"
)

type Format string

const FormatText Format = "text"

type Renderer struct {
	stdout io.Writer
	stderr io.Writer
	format Format
}

func New(stdout, stderr io.Writer, format Format) *Renderer {
	if format == "" {
		format = FormatText
	}
	return &Renderer{
		stdout: stdout,
		stderr: stderr,
		format: format,
	}
}

func (r *Renderer) Format() Format {
	return r.format
}

func (r *Renderer) Text(format string, args ...any) {
	r.write(r.stdout, format, args...)
}

func (r *Renderer) Success(format string, args ...any) {
	r.write(r.stdout, "[OK] "+format, args...)
}

func (r *Renderer) Warning(format string, args ...any) {
	r.write(r.stderr, "[WARN] "+format, args...)
}

func (r *Renderer) Error(format string, args ...any) {
	r.write(r.stderr, "[ERROR] "+format, args...)
}

func (r *Renderer) Table(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = len(sanitizeCell(header))
	}

	for _, row := range rows {
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			if width := len(sanitizeCell(cell)); width > widths[i] {
				widths[i] = width
			}
		}
	}

	r.writeRow(headers, widths)
	separator := make([]string, len(widths))
	for i, width := range widths {
		separator[i] = strings.Repeat("-", width)
	}
	r.writeRow(separator, widths)

	for _, row := range rows {
		r.writeRow(row, widths)
	}
}

func (r *Renderer) writeRow(row []string, widths []int) {
	cells := make([]string, len(widths))
	for i := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		cells[i] = padRight(sanitizeCell(cell), widths[i])
	}
	fmt.Fprintln(r.stdout, strings.Join(cells, "  "))
}

func (r *Renderer) write(writer io.Writer, format string, args ...any) {
	if len(args) == 0 {
		fmt.Fprintln(writer, format)
		return
	}
	fmt.Fprintf(writer, format+"\n", args...)
}

func sanitizeCell(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}
