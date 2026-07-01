package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	laborRatePerHour = 25.0
	docxSuffix       = "_勤工助学学生工作记录表.docx"
)

type excelPerson struct {
	Name          string
	ExcelHours    float64
	ExcelAmount   float64
	SourceCell    string
	RawCellValue  string
	ValueIsAmount bool
}

type recordPerson struct {
	Name       string
	Rows       []recordRow
	RowHours   float64
	TotalHours *float64
	SourceFile string
}

type recordRow struct {
	Year  string
	Month string
	Day   string
	Start string
	End   string
	Hours float64
}

type reportIssue struct {
	Level   string
	Message string
}

type checkReport struct {
	ExcelPath       string
	RecordsPath     string
	ExcelPeople     map[string]excelPerson
	RecordPeople    map[string]recordPerson
	Issues          []reportIssue
	ExcelTotalHours float64
	RecordTotalRows float64
}

type table struct {
	Rows       []row
	MaxColumns int
}

type row struct {
	Cells      []cell
	GridLength int
}

type cell struct {
	Text      string
	GridStart int
	GridSpan  int
}

type columnMap struct {
	Year     int
	Month    int
	Day      int
	Start    int
	End      int
	Hours    int
	Valid    bool
	Fallback bool
}

var numberPattern = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)

func main() {
	excelPath := flag.String("excel", "", "劳务转换导出的 Excel 文件路径")
	recordsPath := flag.String("records", "", "下载记录表得到的 zip 文件路径")
	outputPath := flag.String("out", "", "可选：把检查报告写入指定文本文件")
	flag.Parse()

	if strings.TrimSpace(*excelPath) == "" || strings.TrimSpace(*recordsPath) == "" {
		fmt.Fprintln(os.Stderr, "用法：labor-check -excel <劳务转换Excel> -records <记录表zip> [-out report.txt]")
		os.Exit(2)
	}

	report, err := runCheck(*excelPath, *recordsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "检查失败："+err.Error())
		os.Exit(2)
	}

	text := formatReport(report)
	fmt.Print(text)
	if strings.TrimSpace(*outputPath) != "" {
		if err := os.WriteFile(*outputPath, []byte(text), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "写入报告失败："+err.Error())
			os.Exit(2)
		}
	}

	if hasBlockingIssue(report.Issues) {
		os.Exit(1)
	}
}

func runCheck(excelPath string, recordsPath string) (checkReport, error) {
	excelPeople, err := parseLaborExcel(excelPath)
	if err != nil {
		return checkReport{}, err
	}
	recordPeople, err := parseRecordsZip(recordsPath)
	if err != nil {
		return checkReport{}, err
	}

	report := checkReport{
		ExcelPath:    excelPath,
		RecordsPath:  recordsPath,
		ExcelPeople:  excelPeople,
		RecordPeople: recordPeople,
	}
	buildIssues(&report)
	return report, nil
}

func parseLaborExcel(path string) (map[string]excelPerson, error) {
	workbook, err := excelize.OpenFile(path, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("读取 Excel 失败：%w", err)
	}
	defer workbook.Close()

	sheets := workbook.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("Excel 中没有工作表")
	}
	sheet := sheets[0]
	rows, err := workbook.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("读取 Excel 工作表失败：%w", err)
	}

	result := map[string]excelPerson{}
	for rowIndex := 1; rowIndex < len(rows); rowIndex++ {
		name := strings.TrimSpace(cellAt(rows[rowIndex], 0))
		if name == "" || isTotalLabel(name) {
			continue
		}

		valueText := strings.TrimSpace(cellAt(rows[rowIndex], 6))
		if valueText == "" {
			return nil, fmt.Errorf("Excel 第 %d 行 %s 的 G 列为空，无法读取调整后工时/金额", rowIndex+1, name)
		}
		value, err := parseNumber(valueText)
		if err != nil {
			return nil, fmt.Errorf("Excel 第 %d 行 %s 的 G 列不是数字：%q", rowIndex+1, name, valueText)
		}

		person := excelPerson{
			Name:         name,
			SourceCell:   fmt.Sprintf("G%d", rowIndex+1),
			RawCellValue: valueText,
		}
		if value > 200 {
			person.ValueIsAmount = true
			person.ExcelAmount = value
			person.ExcelHours = value / laborRatePerHour
		} else {
			person.ExcelHours = value
			person.ExcelAmount = value * laborRatePerHour
		}
		result[name] = person
	}

	if len(result) == 0 {
		return nil, errors.New("Excel 中没有读到人员数据，请确认传入的是劳务转换导出的 Excel")
	}
	return result, nil
}

func parseRecordsZip(path string) (map[string]recordPerson, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("读取记录表 zip 失败：%w", err)
	}
	defer reader.Close()

	result := map[string]recordPerson{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(file.Name), ".docx") {
			continue
		}
		name := recordNameFromPath(file.Name)
		if strings.TrimSpace(name) == "" {
			continue
		}
		content, err := readZipFile(file)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败：%w", file.Name, err)
		}
		person, err := parseRecordDocx(name, file.Name, content)
		if err != nil {
			return nil, err
		}
		result[name] = person
	}

	if len(result) == 0 {
		return nil, errors.New("记录表 zip 中没有找到 docx 文件")
	}
	return result, nil
}

func parseRecordDocx(name string, sourceFile string, content []byte) (recordPerson, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return recordPerson{}, fmt.Errorf("%s 不是有效 docx：%w", sourceFile, err)
	}
	var documentXML []byte
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		documentXML, err = readZipFile(file)
		if err != nil {
			return recordPerson{}, fmt.Errorf("读取 %s 的 document.xml 失败：%w", sourceFile, err)
		}
		break
	}
	if len(documentXML) == 0 {
		return recordPerson{}, fmt.Errorf("%s 缺少 word/document.xml", sourceFile)
	}

	tables, err := parseTables(documentXML)
	if err != nil {
		return recordPerson{}, fmt.Errorf("解析 %s 表格失败：%w", sourceFile, err)
	}
	for _, tbl := range tables {
		columns := detectColumns(tbl)
		if !columns.Valid {
			continue
		}
		person := recordPerson{Name: name, SourceFile: sourceFile}
		for _, row := range tbl.Rows {
			if rowContainsTotal(row) {
				total := parseTotalHours(row)
				if total != nil {
					person.TotalHours = total
				}
				continue
			}
			hourText := textByGrid(row, columns.Hours)
			hours, ok := parseOptionalHours(hourText)
			if !ok {
				continue
			}
			record := recordRow{
				Year:  strings.TrimSpace(textByGrid(row, columns.Year)),
				Month: strings.TrimSpace(textByGrid(row, columns.Month)),
				Day:   strings.TrimSpace(textByGrid(row, columns.Day)),
				Start: strings.TrimSpace(textByGrid(row, columns.Start)),
				End:   strings.TrimSpace(textByGrid(row, columns.End)),
				Hours: hours,
			}
			person.Rows = append(person.Rows, record)
			person.RowHours += hours
		}
		return person, nil
	}

	return recordPerson{}, fmt.Errorf("%s 没有识别到勤工助学记录表结构", sourceFile)
}

func parseTables(document []byte) ([]table, error) {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	var tables []table
	var currentTable *table
	var currentRow *row
	var currentCell *cell
	inText := false
	inGridSpan := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "tbl":
				currentTable = &table{}
			case "tr":
				if currentTable != nil {
					currentRow = &row{}
				}
			case "tc":
				if currentRow != nil {
					currentCell = &cell{GridSpan: 1}
				}
			case "gridSpan":
				inGridSpan = true
				if currentCell != nil {
					for _, attr := range typed.Attr {
						if attr.Name.Local == "val" {
							if span, err := strconv.Atoi(strings.TrimSpace(attr.Value)); err == nil && span > 0 {
								currentCell.GridSpan = span
							}
						}
					}
				}
			case "t":
				if currentCell != nil {
					inText = true
				}
			}
		case xml.CharData:
			if inText && currentCell != nil {
				currentCell.Text += string([]byte(typed))
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "t":
				inText = false
			case "gridSpan":
				inGridSpan = false
			case "tc":
				if currentRow != nil && currentCell != nil {
					currentCell.GridStart = currentRow.GridLength
					currentRow.Cells = append(currentRow.Cells, *currentCell)
					currentRow.GridLength += currentCell.GridSpan
					currentCell = nil
				}
			case "tr":
				if currentTable != nil && currentRow != nil {
					currentTable.Rows = append(currentTable.Rows, *currentRow)
					if currentRow.GridLength > currentTable.MaxColumns {
						currentTable.MaxColumns = currentRow.GridLength
					}
					currentRow = nil
				}
			case "tbl":
				if currentTable != nil {
					tables = append(tables, *currentTable)
					currentTable = nil
				}
			}
		}
		_ = inGridSpan
	}
	return tables, nil
}

func detectColumns(tbl table) columnMap {
	headers := make([]string, max(14, tbl.MaxColumns))
	limit := min(3, len(tbl.Rows))
	for rowIndex := 0; rowIndex < limit; rowIndex++ {
		for _, cell := range tbl.Rows[rowIndex].Cells {
			for grid := cell.GridStart; grid < cell.GridStart+cell.GridSpan && grid < len(headers); grid++ {
				text := normalizeText(cell.Text)
				if text != "" && !strings.Contains(headers[grid], text) {
					headers[grid] += text
				}
			}
		}
	}

	columns := columnMap{Year: -1, Month: -1, Day: -1, Start: -1, End: -1, Hours: -1}
	for index, text := range headers {
		switch {
		case strings.Contains(text, "年") && columns.Year < 0:
			columns.Year = index
		case strings.Contains(text, "月") && columns.Month < 0:
			columns.Month = index
		case strings.Contains(text, "日") && columns.Day < 0:
			columns.Day = index
		case strings.Contains(text, "起") && columns.Start < 0:
			columns.Start = index
		case strings.Contains(text, "讫") && columns.End < 0:
			columns.End = index
		case strings.Contains(text, "时数") && columns.Hours < 0:
			columns.Hours = index
		}
	}
	if columns.Year >= 0 && columns.Month >= 0 && columns.Day >= 0 && columns.Start >= 0 && columns.End >= 0 && columns.Hours >= 0 {
		columns.Valid = true
		return columns
	}

	if tbl.MaxColumns >= 14 {
		return columnMap{Year: 0, Month: 1, Day: 2, Start: 8, End: 9, Hours: 11, Valid: true, Fallback: true}
	}
	return columnMap{}
}

func buildIssues(report *checkReport) {
	for name, excelPerson := range report.ExcelPeople {
		report.ExcelTotalHours += excelPerson.ExcelHours
		recordPerson, ok := report.RecordPeople[name]
		if !ok {
			report.Issues = append(report.Issues, reportIssue{"ERROR", fmt.Sprintf("记录表 zip 中缺少人员：%s", name)})
			continue
		}
		diffHours := recordPerson.RowHours - excelPerson.ExcelHours
		if math.Abs(diffHours) > 0.0001 {
			report.Issues = append(report.Issues, reportIssue{"ERROR", fmt.Sprintf("%s 工时不一致：Excel %.2f 小时（约 %.2f 元），记录表 %.2f 小时（约 %.2f 元），差 %.2f 小时（约 %.2f 元）",
				name, excelPerson.ExcelHours, excelPerson.ExcelAmount, recordPerson.RowHours, recordPerson.RowHours*laborRatePerHour, diffHours, diffHours*laborRatePerHour)})
		}
		if recordPerson.TotalHours != nil && math.Abs(*recordPerson.TotalHours-recordPerson.RowHours) > 0.0001 {
			report.Issues = append(report.Issues, reportIssue{"ERROR", fmt.Sprintf("%s 合计格不一致：明细合计 %.2f 小时，合计格 %.2f 小时", name, recordPerson.RowHours, *recordPerson.TotalHours)})
		}
	}

	for name, recordPerson := range report.RecordPeople {
		report.RecordTotalRows += recordPerson.RowHours
		if _, ok := report.ExcelPeople[name]; !ok {
			report.Issues = append(report.Issues, reportIssue{"ERROR", fmt.Sprintf("记录表 zip 中存在 Excel 没有的人员：%s（%.2f 小时，文件 %s）", name, recordPerson.RowHours, recordPerson.SourceFile)})
		}
		if len(recordPerson.Rows) == 0 {
			report.Issues = append(report.Issues, reportIssue{"ERROR", fmt.Sprintf("%s 的记录表没有读到任何明细行", name)})
		}
		if recordPerson.TotalHours == nil {
			report.Issues = append(report.Issues, reportIssue{"WARN", fmt.Sprintf("%s 的记录表没有读到“合计小时数”格", name)})
		}
	}
}

func formatReport(report checkReport) string {
	var builder strings.Builder
	builder.WriteString("劳务转换二次检查报告\n")
	builder.WriteString("====================\n")
	builder.WriteString("Excel: " + report.ExcelPath + "\n")
	builder.WriteString("记录表: " + report.RecordsPath + "\n")
	builder.WriteString(fmt.Sprintf("Excel 人数: %d\n", len(report.ExcelPeople)))
	builder.WriteString(fmt.Sprintf("记录表人数: %d\n", len(report.RecordPeople)))
	builder.WriteString(fmt.Sprintf("Excel 合计: %.2f 小时，约 %.2f 元\n", report.ExcelTotalHours, report.ExcelTotalHours*laborRatePerHour))
	builder.WriteString(fmt.Sprintf("记录表合计: %.2f 小时，约 %.2f 元\n", report.RecordTotalRows, report.RecordTotalRows*laborRatePerHour))
	builder.WriteString("\n")

	if len(report.Issues) == 0 {
		builder.WriteString("结果：通过，Excel 与记录表 zip 的人员和工时金额一致。\n")
		return builder.String()
	}

	builder.WriteString("结果：未通过，请处理以下问题。\n")
	for _, issue := range report.Issues {
		builder.WriteString(fmt.Sprintf("[%s] %s\n", issue.Level, issue.Message))
	}

	builder.WriteString("\n人员明细：\n")
	names := sortedNames(report.ExcelPeople, report.RecordPeople)
	for _, name := range names {
		excelPerson, hasExcel := report.ExcelPeople[name]
		recordPerson, hasRecord := report.RecordPeople[name]
		if hasExcel && hasRecord {
			builder.WriteString(fmt.Sprintf("- %s: Excel %.2f 小时 / 记录表 %.2f 小时 / 差 %.2f 小时\n", name, excelPerson.ExcelHours, recordPerson.RowHours, recordPerson.RowHours-excelPerson.ExcelHours))
		} else if hasExcel {
			builder.WriteString(fmt.Sprintf("- %s: Excel %.2f 小时 / 记录表缺失\n", name, excelPerson.ExcelHours))
		} else {
			builder.WriteString(fmt.Sprintf("- %s: Excel 缺失 / 记录表 %.2f 小时\n", name, recordPerson.RowHours))
		}
	}
	return builder.String()
}

func sortedNames(excelPeople map[string]excelPerson, recordPeople map[string]recordPerson) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(excelPeople)+len(recordPeople))
	for name := range excelPeople {
		seen[name] = true
		names = append(names, name)
	}
	for name := range recordPeople {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func hasBlockingIssue(issues []reportIssue) bool {
	for _, issue := range issues {
		if issue.Level == "ERROR" {
			return true
		}
	}
	return false
}

func readZipFile(file *zip.File) ([]byte, error) {
	handle, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return io.ReadAll(handle)
}

func recordNameFromPath(path string) string {
	name := filepath.Base(filepath.ToSlash(path))
	name = strings.TrimSuffix(name, ".docx")
	name = strings.TrimSuffix(name, docxSuffix[:len(docxSuffix)-len(".docx")])
	if strings.Contains(name, "_") {
		name = strings.SplitN(name, "_", 2)[0]
	}
	return strings.TrimSpace(name)
}

func cellAt(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}

func parseNumber(value string) (float64, error) {
	text := strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if text == "" {
		return 0, errors.New("empty")
	}
	match := numberPattern.FindString(text)
	if match == "" {
		return 0, fmt.Errorf("not a number: %s", value)
	}
	return strconv.ParseFloat(match, 64)
}

func parseOptionalHours(value string) (float64, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, false
	}
	hours, err := parseNumber(text)
	if err != nil {
		return 0, false
	}
	if math.Abs(hours) < 0.0001 {
		return 0, false
	}
	return hours, true
}

func parseTotalHours(row row) *float64 {
	totalEnd := -1
	for _, cell := range row.Cells {
		text := normalizeText(cell.Text)
		if strings.Contains(text, "合计") || strings.Contains(text, "合") {
			totalEnd = max(totalEnd, cell.GridStart+cell.GridSpan)
		}
	}
	for _, cell := range row.Cells {
		if cell.GridStart < totalEnd {
			continue
		}
		hours, ok := parseOptionalHours(cell.Text)
		if ok {
			return &hours
		}
	}
	return nil
}

func rowContainsTotal(row row) bool {
	for _, cell := range row.Cells {
		text := normalizeText(cell.Text)
		if strings.Contains(text, "合计") || strings.Contains(text, "合") && strings.Contains(text, "小时") {
			return true
		}
	}
	return false
}

func textByGrid(row row, gridIndex int) string {
	for _, cell := range row.Cells {
		if gridIndex >= cell.GridStart && gridIndex < cell.GridStart+cell.GridSpan {
			return cell.Text
		}
	}
	return ""
}

func isTotalLabel(value string) bool {
	text := strings.ToLower(normalizeText(value))
	return strings.Contains(text, "合计") ||
		strings.Contains(text, "total") ||
		strings.Contains(text, "鍚堣") ||
		strings.Contains(text, "合")
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}
