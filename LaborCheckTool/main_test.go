package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestRunCheckPassesWhenExcelAndRecordsMatch(t *testing.T) {
	dir := t.TempDir()
	excelPath := filepath.Join(dir, "labor.xlsx")
	recordsPath := filepath.Join(dir, "records.zip")
	writeTestLaborExcel(t, excelPath, 4)
	writeTestRecordsZip(t, recordsPath, "A", 4, "4小时")

	report, err := runCheck(excelPath, recordsPath)
	if err != nil {
		t.Fatalf("runCheck returned error: %v", err)
	}
	if hasBlockingIssue(report.Issues) {
		t.Fatalf("expected no blocking issues, got %#v", report.Issues)
	}
}

func TestRunCheckReportsMismatch(t *testing.T) {
	dir := t.TempDir()
	excelPath := filepath.Join(dir, "labor.xlsx")
	recordsPath := filepath.Join(dir, "records.zip")
	writeTestLaborExcel(t, excelPath, 4)
	writeTestRecordsZip(t, recordsPath, "A", 3, "3小时")

	report, err := runCheck(excelPath, recordsPath)
	if err != nil {
		t.Fatalf("runCheck returned error: %v", err)
	}
	if !hasBlockingIssue(report.Issues) {
		t.Fatalf("expected blocking mismatch issue, got %#v", report.Issues)
	}
	text := formatReport(report)
	if !strings.Contains(text, "工时不一致") {
		t.Fatalf("report should mention mismatch:\n%s", text)
	}
}

func writeTestLaborExcel(t *testing.T, path string, hours float64) {
	t.Helper()
	file := excelize.NewFile()
	defer file.Close()
	sheet := "Sheet1"
	file.SetSheetName("Sheet1", sheet)
	file.SetCellValue(sheet, "A1", "")
	file.SetCellValue(sheet, "G1", "调整后应发")
	file.SetCellValue(sheet, "A2", "A")
	file.SetCellValue(sheet, "G2", hours)
	file.SetCellValue(sheet, "A3", "合计")
	file.SetCellValue(sheet, "G3", hours)
	if err := file.SaveAs(path); err != nil {
		t.Fatalf("write test excel: %v", err)
	}
}

func writeTestRecordsZip(t *testing.T, path string, name string, hours float64, totalText string) {
	t.Helper()
	docx := buildTestRecordDocx(t, hours, totalText)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("6月勤工助学记录表/" + name + docxSuffix)
	if err != nil {
		t.Fatalf("create records entry: %v", err)
	}
	if _, err := entry.Write(docx); err != nil {
		t.Fatalf("write records entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close records zip: %v", err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatalf("write records zip: %v", err)
	}
}

func buildTestRecordDocx(t *testing.T, hours float64, totalText string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	writeZipString(t, writer, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`)
	writeZipString(t, writer, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`)
	writeZipString(t, writer, "word/document.xml", testRecordDocumentXML(hours, totalText))
	if err := writer.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
	return buffer.Bytes()
}

func testRecordDocumentXML(hours float64, totalText string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl>` +
		testRecordRow([]testCell{{text: "工作日期", span: 3}, {text: "工作内容及地点", span: 5}, {text: "工作时间", span: 5}, {text: "教师签名", span: 1}}) +
		testRecordRow([]testCell{{text: "年"}, {text: "月"}, {text: "日"}, {text: "工作内容及地点", span: 5}, {text: "起"}, {text: "讫", span: 2}, {text: "时数", span: 2}, {text: "教师签名"}}) +
		testRecordRow([]testCell{{text: "2026"}, {text: "6"}, {text: "1"}, {text: "机房运维C5-569", span: 5}, {text: "8:00"}, {text: "12:00", span: 2}, {text: formatTestHours(hours), span: 2}, {text: ""}}) +
		testRecordRow([]testCell{{text: "合       计\n       小时", span: 8}, {text: totalText, span: 6}}) +
		`</w:tbl></w:body></w:document>`
}

type testCell struct {
	text string
	span int
}

func testRecordRow(cells []testCell) string {
	var builder strings.Builder
	builder.WriteString("<w:tr>")
	for _, cell := range cells {
		span := cell.span
		if span <= 0 {
			span = 1
		}
		builder.WriteString("<w:tc><w:tcPr>")
		if span > 1 {
			builder.WriteString(`<w:gridSpan w:val="`)
			builder.WriteString(formatTestHours(float64(span)))
			builder.WriteString(`"/>`)
		}
		builder.WriteString("</w:tcPr><w:p><w:r><w:t>")
		builder.WriteString(cell.text)
		builder.WriteString("</w:t></w:r></w:p></w:tc>")
	}
	builder.WriteString("</w:tr>")
	return builder.String()
}

func formatTestHours(value float64) string {
	if value == float64(int64(value)) {
		return strconvFormatInt(int64(value))
	}
	return strconvFormatFloat(value)
}

func strconvFormatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func strconvFormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func writeZipString(t *testing.T, writer *zip.Writer, name string, content string) {
	t.Helper()
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := entry.Write([]byte(content)); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}
