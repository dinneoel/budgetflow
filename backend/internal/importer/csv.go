// Package importer implements the CSV import flow: upload (tolerant parsing
// plus column detection), column mapping with date and amount format options,
// preview with per-row validation and duplicate detection, atomic commit into
// an import batch, and batch undo.
package importer

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	// MaxUploadBytes bounds the accepted CSV file size.
	MaxUploadBytes = 5 << 20
	maxRows        = 10000
	sampleRowCount = 10
)

// parsedFile is the outcome of parsing an uploaded CSV: the detected (or
// synthesized) column names and the data rows, all padded to equal width.
type parsedFile struct {
	Header    []string
	HasHeader bool
	Rows      [][]string
}

func parseCSV(data []byte) (parsedFile, error) {
	text := decodeText(data)
	if strings.TrimSpace(text) == "" {
		return parsedFile{}, ValidationError("the file is empty")
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = detectDelimiter(text)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return parsedFile{}, ValidationError("could not parse the file as CSV: " + err.Error())
	}

	rows := make([][]string, 0, len(records))
	width := 0
	for _, rec := range records {
		empty := true
		for i, cell := range rec {
			rec[i] = strings.TrimSpace(cell)
			if rec[i] != "" {
				empty = false
			}
		}
		if empty {
			continue
		}
		if len(rec) > width {
			width = len(rec)
		}
		rows = append(rows, rec)
	}
	if len(rows) == 0 {
		return parsedFile{}, ValidationError("the file has no data rows")
	}
	if len(rows) > maxRows+1 {
		return parsedFile{}, ValidationError(fmt.Sprintf("the file has too many rows (limit %d)", maxRows))
	}
	for i, row := range rows {
		for len(row) < width {
			row = append(row, "")
		}
		rows[i] = row
	}

	hasHeader := len(rows) > 1 && looksLikeHeader(rows[0])
	header := make([]string, width)
	if hasHeader {
		for i, cell := range rows[0] {
			header[i] = cell
		}
		rows = rows[1:]
	}
	for i, name := range header {
		if name == "" {
			header[i] = fmt.Sprintf("Column %d", i+1)
		}
	}
	return parsedFile{Header: header, HasHeader: hasHeader, Rows: rows}, nil
}

// looksLikeHeader treats the first row as a header when none of its cells
// parses as a date or an amount.
func looksLikeHeader(row []string) bool {
	for _, cell := range row {
		if cell == "" {
			continue
		}
		if _, err := parseDate(cell, "auto"); err == nil {
			return false
		}
		if _, err := parseAmount(cell, "auto"); err == nil {
			return false
		}
	}
	return true
}

// decodeText converts raw upload bytes to a UTF-8 string, handling UTF-8 and
// UTF-16 byte-order marks and falling back to Latin-1 for content that is not
// valid UTF-8.
func decodeText(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		data = data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return decodeUTF16(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return decodeUTF16(data[2:], true)
	}
	if utf8.Valid(data) {
		return string(data)
	}
	// Latin-1: each byte is the code point of the same value.
	rs := make([]rune, len(data))
	for i, b := range data {
		rs[i] = rune(b)
	}
	return string(rs)
}

func decodeUTF16(data []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if bigEndian {
			u = append(u, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			u = append(u, uint16(data[i+1])<<8|uint16(data[i]))
		}
	}
	return string(utf16.Decode(u))
}

// detectDelimiter picks the candidate whose per-line count is non-zero and
// most consistent across the first lines of the file.
func detectDelimiter(text string) rune {
	lines := strings.Split(text, "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	best, bestScore := ',', 0
	for _, cand := range []rune{',', ';', '\t', '|'} {
		first, matching := 0, 0
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			n := strings.Count(line, string(cand))
			if first == 0 {
				first = n
			}
			if n == first && n > 0 {
				matching++
			}
		}
		if score := first * matching; score > bestScore {
			best, bestScore = cand, score
		}
	}
	return best
}

// dateFormats maps the client-facing format options to Go layouts. "auto"
// tries autoDateLayouts in order, so unambiguous ISO dates win and slash
// dates are assumed month-first; clients pick an explicit option otherwise.
// The unpadded Go layout tokens ("1", "2") accept both padded and unpadded
// values, so "3/4/2026" and "03/04/2026" both parse.
var dateFormats = map[string]string{
	"auto":       "",
	"YYYY-MM-DD": "2006-1-2",
	"YYYY/MM/DD": "2006/1/2",
	"MM/DD/YYYY": "1/2/2006",
	"DD/MM/YYYY": "2/1/2006",
	"DD.MM.YYYY": "2.1.2006",
	"MM-DD-YYYY": "1-2-2006",
	"DD-MM-YYYY": "2-1-2006",
}

var autoDateLayouts = []string{
	"2006-1-2", "2006/1/2", "1/2/2006", "2.1.2006", "Jan 2, 2006", "2 Jan 2006",
}

// DateFormatOptions lists the supported dateFormat values for the mapping UI.
func DateFormatOptions() []string {
	return []string{"auto", "YYYY-MM-DD", "YYYY/MM/DD", "MM/DD/YYYY", "DD/MM/YYYY", "DD.MM.YYYY", "MM-DD-YYYY", "DD-MM-YYYY"}
}

// AmountFormatOptions lists the supported amountFormat values.
func AmountFormatOptions() []string { return []string{"auto", "dot_decimal", "comma_decimal"} }

func parseDate(s, format string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("date is empty")
	}
	layout, ok := dateFormats[format]
	if !ok {
		return time.Time{}, fmt.Errorf("unknown date format %q", format)
	}
	if layout != "" {
		t, err := time.Parse(layout, s)
		if err != nil {
			return time.Time{}, fmt.Errorf("date %q does not match %s", s, format)
		}
		return checkDateRange(t)
	}
	for _, l := range autoDateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return checkDateRange(t)
		}
	}
	// a timestamp: retry with just the date portion
	if fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == 'T' }); len(fields) > 1 {
		return parseDate(fields[0], "auto")
	}
	return time.Time{}, fmt.Errorf("unrecognized date %q", s)
}

func checkDateRange(t time.Time) (time.Time, error) {
	if t.Year() < 1900 || t.Year() > 2100 {
		return time.Time{}, fmt.Errorf("date %s is out of range", t.Format("2006-01-02"))
	}
	return t, nil
}

// parseAmount converts a decimal money string to signed minor units.
// Currency symbols and spaces are ignored; parentheses mean negative. Under
// "auto", when both separators appear the rightmost is the decimal point, and
// a lone separator followed by exactly three digits is read as a thousands
// separator (the common bank-export convention).
func parseAmount(s, format string) (int64, error) {
	s = strings.TrimSpace(s)
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && len(s) > 2 {
		neg, s = true, s[1:len(s)-1]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r == ',', r == '.', r == '-', r == '+':
			b.WriteRune(r)
		case r == ' ', r == ' ', r == ' ', r == '$', r == '€', r == '£', r == '₴', r == '¥':
			// ignore currency symbols and (non-breaking) spaces
		default:
			return 0, fmt.Errorf("unrecognized character %q in amount", r)
		}
	}
	s = b.String()
	if s == "" {
		return 0, fmt.Errorf("amount has no digits")
	}
	switch s[0] {
	case '-':
		neg, s = !neg, s[1:]
	case '+':
		s = s[1:]
	}
	if s == "" || strings.ContainsAny(s, "-+") {
		return 0, fmt.Errorf("malformed amount")
	}

	var dec byte
	switch format {
	case "dot_decimal":
		dec = '.'
	case "comma_decimal":
		dec = ','
	case "auto", "":
		lastDot, lastComma := strings.LastIndexByte(s, '.'), strings.LastIndexByte(s, ',')
		switch {
		case lastDot >= 0 && lastComma >= 0:
			if lastDot > lastComma {
				dec = '.'
			} else {
				dec = ','
			}
		case lastDot >= 0 && (strings.Count(s, ".") > 1 || len(s)-lastDot-1 == 3):
			dec = 0 // thousands separators only
		case lastDot >= 0:
			dec = '.'
		case lastComma >= 0 && (strings.Count(s, ",") > 1 || len(s)-lastComma-1 == 3):
			dec = 0
		case lastComma >= 0:
			dec = ','
		}
	default:
		return 0, fmt.Errorf("unknown amount format %q", format)
	}

	whole, frac := s, ""
	if dec != 0 {
		if strings.Count(s, string(dec)) > 1 {
			return 0, fmt.Errorf("malformed amount %q", s)
		}
		if i := strings.LastIndexByte(s, dec); i >= 0 {
			whole, frac = s[:i], s[i+1:]
		}
	}
	if dec == ',' {
		whole = strings.ReplaceAll(whole, ".", "")
	} else {
		whole = strings.ReplaceAll(whole, ",", "")
		if dec == 0 { // thousands separators only, either kind
			whole = strings.ReplaceAll(whole, ".", "")
		}
	}
	if whole == "" {
		whole = "0"
	}
	if strings.ContainsAny(whole, ".,") || strings.ContainsAny(frac, ".,") {
		return 0, fmt.Errorf("malformed amount %q", s)
	}
	if len(frac) > 2 {
		return 0, fmt.Errorf("amounts may have at most 2 decimal places")
	}
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("malformed amount %q", s)
	}
	cents := int64(0)
	if frac != "" {
		if cents, err = strconv.ParseInt(frac, 10, 64); err != nil {
			return 0, fmt.Errorf("malformed amount %q", s)
		}
		if len(frac) == 1 {
			cents *= 10
		}
	}
	if units > (math.MaxInt64-cents)/100 {
		return 0, fmt.Errorf("amount %q is too large", s)
	}
	minor := units*100 + cents
	if neg {
		minor = -minor
	}
	return minor, nil
}
