package store

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"strconv"
	"strings"
	"testing"
)

func TestParseWorkStudyCSVGroupsSortsAndSkipsTotals(t *testing.T) {
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	rows := [][]string{
		{"\u59d3\u540d", "\u5e74", "\u6708", "\u65e5", "\u8d77", "\u8bab", "\u65f6\u6570"},
		{"A", "2026", "5", "2", "14:00", "18:00", "4.0"},
		{"A", "2026", "5", "1", "8:00", "12:00", "4.0"},
		{"A", "\u5408\u8ba1", "", "", "", "", "8.0"},
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			t.Fatalf("write csv row: %v", err)
		}
	}
	writer.Flush()

	grouped, err := parseWorkStudyCSV(buffer.Bytes())
	if err != nil {
		t.Fatalf("parseWorkStudyCSV returned error: %v", err)
	}
	if got := len(grouped["A"]); got != 2 {
		t.Fatalf("records for A = %d, want 2", got)
	}
	if grouped["A"][0].Day != 1 || grouped["A"][0].Start != "8:00" {
		t.Fatalf("records were not sorted by date/start: %#v", grouped["A"])
	}
}

func TestCreateWorkStudyRecordsZipMissingStudentNumberListsNames(t *testing.T) {
	_, err := createWorkStudyRecordsZip(map[string][]workStudyRecord{
		"A": {{Name: "A", Year: 2026, Month: 6, Day: 1, Start: "8:00", End: "12:00", Hours: "4.0"}},
	}, buildWorkStudyTemplateDocx(t, 2), map[string]string{}, workStudyDefaultContent, mustCSVMonth(t, "2026-06"))
	if err == nil {
		t.Fatal("expected missing student number error")
	}
	missing, ok := err.(WorkStudyMissingStudentNumbersError)
	if !ok {
		t.Fatalf("error type = %T, want WorkStudyMissingStudentNumbersError", err)
	}
	if len(missing.Names) != 1 || missing.Names[0] != "A" {
		t.Fatalf("missing names = %#v, want A", missing.Names)
	}
}

func TestFillWorkStudyTemplateWritesRecordsAndClearsMetadata(t *testing.T) {
	template := buildWorkStudyTemplateDocx(t, 2)
	output, err := fillWorkStudyTemplate(template, "测试姓名", "202600000001", []workStudyRecord{
		{Name: "A", Year: 2026, Month: 6, Day: 8, Start: "8:00", End: "12:00", Hours: "4.0"},
	}, workStudyDefaultContent)
	if err != nil {
		t.Fatalf("fillWorkStudyTemplate returned error: %v", err)
	}

	documentXML := readDocxEntry(t, output, "word/document.xml")
	for _, want := range []string{"测试姓名", "202600000001", "2026", "6", "8", workStudyDefaultContent, "8:00", "12:00", "<w:t>4</w:t>", "4\u5c0f\u65f6"} {
		if !strings.Contains(documentXML, want) {
			t.Fatalf("document.xml does not contain %q:\n%s", want, documentXML)
		}
	}
	for _, placeholder := range []string{workStudyNamePlaceholder, workStudyStudentNumberPlaceholder} {
		if strings.Contains(documentXML, placeholder) {
			t.Fatalf("document.xml still contains placeholder %q", placeholder)
		}
	}
	if strings.Contains(documentXML, "4.0") {
		t.Fatalf("document.xml should not contain decimal hours:\n%s", documentXML)
	}
	dataHoursCellXML := findWorkStudyTestCellXML(t, documentXML, "<w:t>4</w:t>")
	for _, unwanted := range []string{"\u6977\u4f53_GB2312", `<w:sz w:val="28"/>`} {
		if strings.Contains(dataHoursCellXML, unwanted) {
			t.Fatalf("data hours cell should keep template/default style, found %q:\n%s", unwanted, dataHoursCellXML)
		}
	}

	totalHoursCellXML := findWorkStudyTestCellByText(t, documentXML, "4\u5c0f\u65f6")
	for _, want := range []string{`<w:rFonts w:ascii="` + "\u6977\u4f53_GB2312" + `" w:eastAsia="` + "\u6977\u4f53_GB2312" + `" w:hint="eastAsia"/>`, `<w:sz w:val="28"/>`} {
		if !strings.Contains(totalHoursCellXML, want) {
			t.Fatalf("total hours cell does not contain style %q:\n%s", want, totalHoursCellXML)
		}
	}

	coreXML := readDocxEntry(t, output, "docProps/core.xml")
	for _, removed := range []string{"Alice", "Bob", "Old title", "Old comments", "Old keywords", "Old category"} {
		if strings.Contains(coreXML, removed) {
			t.Fatalf("core.xml still contains %q:\n%s", removed, coreXML)
		}
	}
}

func TestFillWorkStudyTemplateErrorsWhenRowsOverflow(t *testing.T) {
	template := buildWorkStudyTemplateDocx(t, 1)
	_, err := fillWorkStudyTemplate(template, "A", "202600000001", []workStudyRecord{
		{Name: "A", Year: 2026, Month: 6, Day: 8, Start: "8:00", End: "12:00", Hours: "4.0"},
		{Name: "A", Year: 2026, Month: 6, Day: 9, Start: "8:00", End: "12:00", Hours: "4.0"},
	}, workStudyDefaultContent)
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !strings.Contains(err.Error(), "\u8d85\u8fc7\u6a21\u677f\u53ef\u5199\u5165\u884c\u6570") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateWorkStudyRecordsZipUsesExpectedDirectoryAndFile(t *testing.T) {
	archive, err := createWorkStudyRecordsZip(map[string][]workStudyRecord{
		"A": {{Name: "A", Year: 2026, Month: 6, Day: 1, Start: "8:00", End: "12:00", Hours: "4.0"}},
		"B": {{Name: "B", Year: 2026, Month: 6, Day: 2, Start: "14:00", End: "18:00", Hours: "4.0"}},
	}, buildWorkStudyTemplateDocx(t, 2), map[string]string{"A": "202600000001", "B": "202600000002"}, workStudyDefaultContent, mustCSVMonth(t, "2026-06"))
	if err != nil {
		t.Fatalf("createWorkStudyRecordsZip returned error: %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("generated zip cannot be opened: %v", err)
	}
	wants := map[string]string{
		"6" + workStudyZipSuffix + "/A_" + workStudyTemplateSuffix: "202600000001",
		"6" + workStudyZipSuffix + "/B_" + workStudyTemplateSuffix: "202600000002",
	}
	for _, file := range reader.File {
		studentNumber, ok := wants[file.Name]
		if !ok {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		docx, err := io.ReadAll(handle)
		handle.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(readDocxEntry(t, docx, "word/document.xml"), studentNumber) {
			t.Fatalf("%s does not contain student number %s", file.Name, studentNumber)
		}
		delete(wants, file.Name)
	}
	if len(wants) > 0 {
		t.Fatalf("zip missing files %v; files=%v", wants, zipFileNames(reader.File))
	}
}

func buildWorkStudyTemplateDocx(t *testing.T, dataRows int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	mustCreateZipEntry(t, writer, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`)
	mustCreateZipEntry(t, writer, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`)
	mustCreateZipEntry(t, writer, "word/document.xml", workStudyTemplateDocumentXML(dataRows))
	mustCreateZipEntry(t, writer, "docProps/core.xml", `<?xml version="1.0" encoding="UTF-8"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:creator>Alice</dc:creator><cp:lastModifiedBy>Bob</cp:lastModifiedBy><dc:title>Old title</dc:title><dc:subject>Old subject</dc:subject><dc:description>Old comments</dc:description><cp:keywords>Old keywords</cp:keywords><cp:category>Old category</cp:category></cp:coreProperties>`)
	if err := writer.Close(); err != nil {
		t.Fatalf("close template zip: %v", err)
	}
	return buffer.Bytes()
}

func workStudyTemplateDocumentXML(dataRows int) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	builder.WriteString(`<w:p><w:r><w:t>学生学号：` + workStudyStudentNumberPlaceholder + `    姓名：` + workStudyNamePlaceholder + `</w:t></w:r></w:p><w:tbl>`)
	builder.WriteString(workStudyTemplateRow([]workStudyTestCell{
		{text: "\u5de5\u4f5c\u65e5\u671f", span: 3},
		{text: "\u5de5\u4f5c\u5185\u5bb9\u53ca\u5730\u70b9", span: 5},
		{text: "\u5de5\u4f5c\u65f6\u95f4", span: 5},
		{text: "\u6559\u5e08\u7b7e\u540d", span: 1},
	}))
	builder.WriteString(workStudyTemplateRow([]workStudyTestCell{
		{text: "\u5e74", span: 1},
		{text: "\u6708", span: 1},
		{text: "\u65e5", span: 1},
		{text: "\u5de5\u4f5c\u5185\u5bb9\u53ca\u5730\u70b9", span: 5},
		{text: "\u8d77", span: 1},
		{text: "\u8bab", span: 2},
		{text: "\u65f6\u6570", span: 2},
		{text: "\u6559\u5e08\u7b7e\u540d", span: 1},
	}))
	for i := 0; i < dataRows; i++ {
		builder.WriteString(workStudyTemplateRow([]workStudyTestCell{
			{span: 1}, {span: 1}, {span: 1}, {span: 5}, {span: 1}, {span: 2}, {span: 2}, {span: 1},
		}))
	}
	builder.WriteString(workStudyTemplateRow([]workStudyTestCell{
		{text: "\u5408       \u8ba1\n       \u5c0f\u65f6", span: 8},
		{text: "               \u5c0f\u65f6", span: 6},
	}))
	builder.WriteString(`</w:tbl></w:body></w:document>`)
	return builder.String()
}

func findWorkStudyTestCellXML(t *testing.T, documentXML string, needle string) string {
	t.Helper()
	tables, err := parseWorkStudyTables([]byte(documentXML))
	if err != nil {
		t.Fatalf("parse generated document.xml: %v", err)
	}
	for _, table := range tables {
		for _, row := range table.Rows {
			for _, cell := range row.Cells {
				cellXML := documentXML[cell.Range.Start:cell.Range.End]
				if strings.Contains(cellXML, needle) {
					return cellXML
				}
			}
		}
	}
	t.Fatalf("missing cell XML containing %q:\n%s", needle, documentXML)
	return ""
}

func findWorkStudyTestCellByText(t *testing.T, documentXML string, text string) string {
	t.Helper()
	tables, err := parseWorkStudyTables([]byte(documentXML))
	if err != nil {
		t.Fatalf("parse generated document.xml: %v", err)
	}
	for _, table := range tables {
		for _, row := range table.Rows {
			for _, cell := range row.Cells {
				if strings.Contains(cell.Text, text) {
					return documentXML[cell.Range.Start:cell.Range.End]
				}
			}
		}
	}
	t.Fatalf("missing cell text containing %q:\n%s", text, documentXML)
	return ""
}

type workStudyTestCell struct {
	text string
	span int
}

func workStudyTemplateRow(cells []workStudyTestCell) string {
	var builder strings.Builder
	builder.WriteString("<w:tr>")
	for _, cell := range cells {
		span := cell.span
		if span <= 0 {
			span = 1
		}
		builder.WriteString(`<w:tc><w:tcPr>`)
		if span > 1 {
			builder.WriteString(`<w:gridSpan w:val="` + strconv.Itoa(span) + `"/>`)
		}
		builder.WriteString(`</w:tcPr><w:p><w:r><w:t>`)
		builder.WriteString(cell.text)
		builder.WriteString(`</w:t></w:r></w:p></w:tc>`)
	}
	builder.WriteString("</w:tr>")
	return builder.String()
}

func mustCreateZipEntry(t *testing.T, writer *zip.Writer, name string, content string) {
	t.Helper()
	handle, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := handle.Write([]byte(content)); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}

func readDocxEntry(t *testing.T, content []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer handle.Close()
		data, err := io.ReadAll(handle)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(data)
	}
	t.Fatalf("missing docx entry %s", name)
	return ""
}

func zipFileNames(files []*zip.File) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}
