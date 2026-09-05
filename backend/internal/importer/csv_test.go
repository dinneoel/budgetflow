package importer

import (
	"strings"
	"testing"
)

func TestParseAmount(t *testing.T) {
	tests := []struct {
		in     string
		format string
		want   int64
		ok     bool
	}{
		{"12.50", "auto", 1250, true},
		{"12", "auto", 1200, true},
		{"12.5", "auto", 1250, true},
		{"-7.25", "auto", -725, true},
		{"+7.25", "auto", 725, true},
		{"(12.00)", "auto", -1200, true},
		{"$5.00", "auto", 500, true},
		{"€ 1 234,56", "auto", 123456, true},
		{"1,234.56", "auto", 123456, true},
		{"1.234,56", "auto", 123456, true},
		{"1,234", "auto", 123400, true},     // lone separator + 3 digits = thousands
		{"1.234", "auto", 123400, true},     // same, dot variant
		{"1.234.567", "auto", 123456700, true},
		{"12,5", "auto", 1250, true},
		{"1,234.56", "dot_decimal", 123456, true},
		{"1.234", "dot_decimal", 0, false}, // 3 decimal places
		{"1.234,5", "comma_decimal", 123450, true},
		{"-7,50", "comma_decimal", -750, true},
		{"", "auto", 0, false},
		{"abc", "auto", 0, false},
		{"12.34.56", "dot_decimal", 0, false},
		{"1-2", "auto", 0, false},
		{"5", "bogus", 0, false},
	}
	for _, tt := range tests {
		got, err := parseAmount(tt.in, tt.format)
		if tt.ok && (err != nil || got != tt.want) {
			t.Errorf("parseAmount(%q, %q) = %d, %v; want %d", tt.in, tt.format, got, err, tt.want)
		}
		if !tt.ok && err == nil {
			t.Errorf("parseAmount(%q, %q) = %d, want error", tt.in, tt.format, got)
		}
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in     string
		format string
		want   string
		ok     bool
	}{
		{"2026-08-31", "auto", "2026-08-31", true},
		{"2026/08/31", "auto", "2026-08-31", true},
		{"08/31/2026", "auto", "2026-08-31", true},
		{"03/04/2026", "auto", "2026-03-04", true}, // auto assumes month-first
		{"31.08.2026", "auto", "2026-08-31", true},
		{"2026-08-31 14:22:05", "auto", "2026-08-31", true},
		{"31/08/2026", "DD/MM/YYYY", "2026-08-31", true},
		{"3/4/2026", "DD/MM/YYYY", "2026-04-03", true}, // unpadded values accepted
		{"31-08-2026", "DD-MM-YYYY", "2026-08-31", true},
		{"31/08/2026", "MM/DD/YYYY", "", false}, // no 31st month
		{"0002-01-01", "auto", "", false},       // out of range
		{"not a date", "auto", "", false},
		{"", "auto", "", false},
		{"2026-08-31", "bogus", "", false},
	}
	for _, tt := range tests {
		got, err := parseDate(tt.in, tt.format)
		if tt.ok && (err != nil || got.Format("2006-01-02") != tt.want) {
			t.Errorf("parseDate(%q, %q) = %v, %v; want %s", tt.in, tt.format, got, err, tt.want)
		}
		if !tt.ok && err == nil {
			t.Errorf("parseDate(%q, %q) = %v, want error", tt.in, tt.format, got)
		}
	}
}

func TestParseCSVDelimitersAndHeader(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		header    []string
		hasHeader bool
		rows      int
	}{
		{
			"comma with header",
			"Date,Amount,Payee\n2026-08-01,-12.50,Coffee\n2026-08-02,-8.00,Lunch\n",
			[]string{"Date", "Amount", "Payee"}, true, 2,
		},
		{
			"semicolon with header",
			"Date;Amount;Payee\n2026-08-01;-12,50;Coffee\n",
			[]string{"Date", "Amount", "Payee"}, true, 1,
		},
		{
			"tab separated",
			"Date\tAmount\n2026-08-01\t-12.50\n",
			[]string{"Date", "Amount"}, true, 1,
		},
		{
			"no header row",
			"2026-08-01,-12.50,Coffee\n2026-08-02,-8.00,Lunch\n",
			[]string{"Column 1", "Column 2", "Column 3"}, false, 2,
		},
		{
			"quoted field with embedded delimiter",
			"Date,Amount,Payee\n2026-08-01,-12.50,\"Coffee, LLC\"\n",
			[]string{"Date", "Amount", "Payee"}, true, 1,
		},
		{
			"ragged rows padded",
			"Date,Amount,Payee\n2026-08-01,-12.50\n",
			[]string{"Date", "Amount", "Payee"}, true, 1,
		},
		{
			"utf-8 BOM stripped",
			"\ufeffDate,Amount\n2026-08-01,-1.00\n",
			[]string{"Date", "Amount"}, true, 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pf, err := parseCSV([]byte(tt.in))
			if err != nil {
				t.Fatalf("parseCSV: %v", err)
			}
			if strings.Join(pf.Header, "|") != strings.Join(tt.header, "|") {
				t.Errorf("header = %v, want %v", pf.Header, tt.header)
			}
			if pf.HasHeader != tt.hasHeader {
				t.Errorf("hasHeader = %v, want %v", pf.HasHeader, tt.hasHeader)
			}
			if len(pf.Rows) != tt.rows {
				t.Errorf("rows = %d, want %d", len(pf.Rows), tt.rows)
			}
			for _, row := range pf.Rows {
				if len(row) != len(pf.Header) {
					t.Errorf("row width %d != header width %d", len(row), len(pf.Header))
				}
			}
		})
	}
}

func TestParseCSVEncodings(t *testing.T) {
	// Latin-1: "Café" with 0xE9, not valid UTF-8
	pf, err := parseCSV([]byte("Date,Amount,Payee\n2026-08-01,-1.00,Caf\xe9\n"))
	if err != nil {
		t.Fatalf("parseCSV latin-1: %v", err)
	}
	if pf.Rows[0][2] != "Café" {
		t.Errorf("latin-1 payee = %q, want Café", pf.Rows[0][2])
	}

	// UTF-16LE with BOM
	src := "Date,Amount\n2026-08-01,-1.00\n"
	buf := []byte{0xFF, 0xFE}
	for _, r := range src {
		buf = append(buf, byte(r), 0)
	}
	pf, err = parseCSV(buf)
	if err != nil {
		t.Fatalf("parseCSV utf-16le: %v", err)
	}
	if len(pf.Rows) != 1 || pf.Rows[0][1] != "-1.00" {
		t.Errorf("utf-16le rows = %v", pf.Rows)
	}
}

func TestParseCSVErrors(t *testing.T) {
	for _, in := range []string{"", "   \n  \n"} {
		if _, err := parseCSV([]byte(in)); err == nil {
			t.Errorf("parseCSV(%q) succeeded, want error", in)
		}
	}
}
