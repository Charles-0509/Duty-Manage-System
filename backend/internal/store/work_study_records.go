package store

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"personnel-management-go/internal/config"
)

const (
	workStudyDefaultContent = "\u673a\u623f\u8fd0\u7ef4C5-569"
	workStudyTemplateSuffix = "\u52e4\u5de5\u52a9\u5b66\u5b66\u751f\u5de5\u4f5c\u8bb0\u5f55\u8868.docx"
	workStudyZipSuffix      = "\u6708\u52e4\u5de5\u52a9\u5b66\u8bb0\u5f55\u8868"

	workStudyColYear    = "year"
	workStudyColMonth   = "month"
	workStudyColDay     = "day"
	workStudyColContent = "content"
	workStudyColStart   = "start"
	workStudyColEnd     = "end"
	workStudyColHours   = "hours"
)

type WorkStudyMissingStudentNumbersError struct {
	Names []string
}

func (e WorkStudyMissingStudentNumbersError) Error() string {
	return fmt.Sprintf("以下成员尚未维护学号：%s", strings.Join(e.Names, "、"))
}

type WorkStudyGlobalTemplateMissingError struct{}

func (WorkStudyGlobalTemplateMissingError) Error() string {
	return "尚未配置全局勤工助学记录表模板"
}

type workStudyRecord struct {
	Name  string
	Year  int
	Month int
	Day   int
	Start string
	End   string
	Hours string
}

type workStudyXMLRange struct {
	Start       int
	StartTagEnd int
	End         int
}

type workStudyTable struct {
	Range      workStudyXMLRange
	Rows       []workStudyRow
	MaxColumns int
}

type workStudyRow struct {
	Range      workStudyXMLRange
	Cells      []workStudyCell
	GridLength int
}

type workStudyCell struct {
	Range     workStudyXMLRange
	Text      string
	GridStart int
	GridSpan  int
}

type workStudyReplacement struct {
	Start int
	End   int
	Text  []byte
}

type workStudyTextStyle int

const (
	workStudyTextStyleDefault workStudyTextStyle = iota
	workStudyTextStyleTotalHours
)

func (s *Store) GetLaborConversionRecordsZip(id string) (string, []byte, error) {
	if !laborHistoryIDPattern.MatchString(id) {
		return "", nil, sql.ErrNoRows
	}

	var csvContent []byte
	var csvOutputMonth string
	var peoplePayload string
	err := s.db.QueryRow(`
		SELECT csv_blob, csv_output_month, people_json
		FROM labor_conversion_runs
		WHERE id = ? AND csv_blob IS NOT NULL
	`, id).Scan(&csvContent, &csvOutputMonth, &peoplePayload)
	if err != nil {
		return "", nil, err
	}

	recordsByName, err := parseWorkStudyCSV(csvContent)
	if err != nil {
		return "", nil, err
	}
	studentNumbers, err := s.resolveWorkStudyStudentNumbers(recordsByName, peoplePayload)
	if err != nil {
		return "", nil, err
	}
	_, templateContent, err := s.GetWorkStudyTemplate()
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, WorkStudyGlobalTemplateMissingError{}
		}
		return "", nil, err
	}

	content := strings.TrimSpace(s.cfg.WorkStudyContent)
	if content == "" {
		content = workStudyDefaultContent
	}

	outputMonth := workStudyOutputMonth(csvOutputMonth, recordsByName)
	filename := fmt.Sprintf("%d%s.zip", int(outputMonth.Month()), workStudyZipSuffix)
	archive, err := createWorkStudyRecordsZip(recordsByName, templateContent, studentNumbers, content, outputMonth)
	if err != nil {
		return "", nil, err
	}
	return filename, archive, nil
}

func (s *Store) resolveWorkStudyStudentNumbers(recordsByName map[string][]workStudyRecord, peoplePayload string) (map[string]string, error) {
	studentNumbers := map[string]string{}
	if strings.TrimSpace(peoplePayload) != "" {
		var people []laborPerson
		if err := json.Unmarshal([]byte(peoplePayload), &people); err != nil {
			return nil, fmt.Errorf("劳务历史人员快照无法读取：%w", err)
		}
		for _, person := range people {
			name := strings.TrimSpace(person.Name)
			studentNumber := strings.TrimSpace(person.StudentNumber)
			if _, wanted := recordsByName[name]; wanted && validateStudentNumber(studentNumber) == nil && studentNumber != "" {
				studentNumbers[name] = studentNumber
			}
		}
	}

	rows, err := s.db.Query(`SELECT real_name, student_number FROM users WHERE role != 'ADMIN'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var realName, studentNumber string
		if err := rows.Scan(&realName, &studentNumber); err != nil {
			return nil, err
		}
		realName = strings.TrimSpace(realName)
		studentNumber = strings.TrimSpace(studentNumber)
		if _, wanted := recordsByName[realName]; !wanted || studentNumbers[realName] != "" {
			continue
		}
		if validateStudentNumber(studentNumber) == nil && studentNumber != "" {
			studentNumbers[realName] = studentNumber
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	missing := make([]string, 0)
	for _, name := range sortedWorkStudyNames(recordsByName) {
		if studentNumbers[name] == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, WorkStudyMissingStudentNumbersError{Names: missing}
	}
	return studentNumbers, nil
}

func (s *Store) workStudyTemplateDir() (string, error) {
	dir := strings.TrimSpace(s.cfg.WorkStudyTemplateDir)
	if dir == "" {
		dir = "../data/work-study/templates"
	}
	dir = os.ExpandEnv(dir)
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir), nil
	}

	base := "."
	if strings.TrimSpace(s.cfg.EnvFilePath) != "" {
		base = filepath.Dir(s.cfg.EnvFilePath)
	}
	return filepath.Abs(filepath.Join(base, dir))
}

func parseWorkStudyCSV(content []byte) (map[string][]workStudyRecord, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(content, []byte("\xEF\xBB\xBF"))))
	reader.FieldsPerRecord = -1

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("\u52b3\u52a1 CSV \u683c\u5f0f\u9519\u8bef\uff1a%w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("\u52b3\u52a1 CSV \u4e3a\u7a7a")
	}

	result := map[string][]workStudyRecord{}
	for rowIndex, row := range rows[1:] {
		if len(row) < 7 {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSpace(row[1])); err != nil {
			continue
		}

		name := strings.TrimSpace(row[0])
		if name == "" {
			continue
		}
		year, err := strconv.Atoi(strings.TrimSpace(row[1]))
		if err != nil {
			return nil, fmt.Errorf("CSV \u7b2c %d \u884c\u5e74\u4efd\u65e0\u6548", rowIndex+2)
		}
		month, err := strconv.Atoi(strings.TrimSpace(row[2]))
		if err != nil || month < 1 || month > 12 {
			return nil, fmt.Errorf("CSV \u7b2c %d \u884c\u6708\u4efd\u65e0\u6548", rowIndex+2)
		}
		day, err := strconv.Atoi(strings.TrimSpace(row[3]))
		if err != nil || day < 1 || day > 31 {
			return nil, fmt.Errorf("CSV \u7b2c %d \u884c\u65e5\u671f\u65e0\u6548", rowIndex+2)
		}
		start := strings.TrimSpace(row[4])
		end := strings.TrimSpace(row[5])
		if _, ok := parseCSVMinute(start); !ok {
			return nil, fmt.Errorf("CSV \u7b2c %d \u884c\u5f00\u59cb\u65f6\u95f4\u65e0\u6548", rowIndex+2)
		}
		if _, ok := parseCSVMinute(end); !ok {
			return nil, fmt.Errorf("CSV \u7b2c %d \u884c\u7ed3\u675f\u65f6\u95f4\u65e0\u6548", rowIndex+2)
		}
		hours := strings.TrimSpace(row[6])
		if _, err := strconv.ParseFloat(hours, 64); err != nil {
			return nil, fmt.Errorf("CSV \u7b2c %d \u884c\u65f6\u6570\u65e0\u6548", rowIndex+2)
		}

		result[name] = append(result[name], workStudyRecord{
			Name:  name,
			Year:  year,
			Month: month,
			Day:   day,
			Start: start,
			End:   end,
			Hours: hours,
		})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("\u52b3\u52a1 CSV \u4e2d\u6ca1\u6709\u53ef\u5199\u5165\u7684\u8bb0\u5f55")
	}

	for name := range result {
		sort.Slice(result[name], func(i, j int) bool {
			left := result[name][i]
			right := result[name][j]
			leftDate := time.Date(left.Year, time.Month(left.Month), left.Day, 0, 0, 0, 0, time.UTC)
			rightDate := time.Date(right.Year, time.Month(right.Month), right.Day, 0, 0, 0, 0, time.UTC)
			if !leftDate.Equal(rightDate) {
				return leftDate.Before(rightDate)
			}
			leftMinute, _ := parseCSVMinute(left.Start)
			rightMinute, _ := parseCSVMinute(right.Start)
			if leftMinute != rightMinute {
				return leftMinute < rightMinute
			}
			return left.End < right.End
		})
	}
	return result, nil
}

func workStudyOutputMonth(value string, recordsByName map[string][]workStudyRecord) time.Time {
	if parsed, err := parseLaborCSVOutputMonth(value); err == nil && strings.TrimSpace(value) != "" {
		return parsed
	}
	for _, records := range recordsByName {
		if len(records) == 0 {
			continue
		}
		record := records[0]
		return time.Date(record.Year, time.Month(record.Month), 1, 0, 0, 0, 0, time.UTC)
	}
	now := time.Now()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func createWorkStudyRecordsZip(recordsByName map[string][]workStudyRecord, templateContent []byte, studentNumbers map[string]string, content string, outputMonth time.Time) ([]byte, error) {
	names := sortedWorkStudyNames(recordsByName)
	missing := make([]string, 0)
	for _, name := range names {
		studentNumber := strings.TrimSpace(studentNumbers[name])
		if studentNumber == "" || validateStudentNumber(studentNumber) != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, WorkStudyMissingStudentNumbersError{Names: missing}
	}

	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)
	dirName := fmt.Sprintf("%d%s", int(outputMonth.Month()), workStudyZipSuffix)
	if _, err := zipWriter.Create(dirName + "/"); err != nil {
		zipWriter.Close()
		return nil, err
	}

	for _, name := range names {
		filled, err := fillWorkStudyTemplate(templateContent, name, studentNumbers[name], recordsByName[name], content)
		if err != nil {
			zipWriter.Close()
			return nil, fmt.Errorf("%s\uff1a%w", name, err)
		}

		entryName := fmt.Sprintf("%s/%s_%s", dirName, name, workStudyTemplateSuffix)
		writer, err := zipWriter.Create(entryName)
		if err != nil {
			zipWriter.Close()
			return nil, err
		}
		if _, err := writer.Write(filled); err != nil {
			zipWriter.Close()
			return nil, err
		}
	}

	if err := zipWriter.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func sortedWorkStudyNames(recordsByName map[string][]workStudyRecord) []string {
	names := make([]string, 0, len(recordsByName))
	for name := range recordsByName {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return config.LessRealName(names[i], names[j])
	})
	return names
}

func fillWorkStudyTemplate(templateContent []byte, name, studentNumber string, records []workStudyRecord, content string) ([]byte, error) {
	return rewriteWorkStudyDOCX(templateContent, func(document []byte) ([]byte, error) {
		if !bytes.Contains(document, []byte(workStudyNamePlaceholder)) || !bytes.Contains(document, []byte(workStudyStudentNumberPlaceholder)) {
			return nil, fmt.Errorf("全局模板缺少姓名或学号占位符")
		}
		document = bytes.ReplaceAll(document, []byte(workStudyNamePlaceholder), []byte(escapeWorkStudyXMLText(name)))
		document = bytes.ReplaceAll(document, []byte(workStudyStudentNumberPlaceholder), []byte(escapeWorkStudyXMLText(studentNumber)))
		return patchWorkStudyDocumentXML(document, records, content)
	})
}

func readZipFile(file *zip.File) ([]byte, error) {
	handle, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return io.ReadAll(handle)
}

func patchWorkStudyDocumentXML(document []byte, records []workStudyRecord, content string) ([]byte, error) {
	table, totalRowIndex, err := locateWorkStudyDataTable(document)
	if err != nil {
		return nil, err
	}
	columnMap := detectWorkStudyColumns(table)
	if err := validateWorkStudyColumns(columnMap); err != nil {
		return nil, err
	}

	clearStart := 2
	clearEnd := totalRowIndex
	if clearEnd < clearStart {
		return nil, fmt.Errorf("\u6a21\u677f\u6570\u636e\u884c\u533a\u57df\u65e0\u6548")
	}
	if len(records) > clearEnd-clearStart {
		return nil, fmt.Errorf("\u8bb0\u5f55\u6570 %d \u8d85\u8fc7\u6a21\u677f\u53ef\u5199\u5165\u884c\u6570 %d", len(records), clearEnd-clearStart)
	}

	replacements := map[string]workStudyReplacement{}
	for rowIndex := clearStart; rowIndex < clearEnd; rowIndex++ {
		for _, cell := range table.Rows[rowIndex].Cells {
			addWorkStudyCellReplacement(replacements, document, cell, "", workStudyTextStyleDefault)
		}
	}

	for index, record := range records {
		row := table.Rows[clearStart+index]
		values := map[string]string{
			workStudyColYear:    strconv.Itoa(record.Year),
			workStudyColMonth:   strconv.Itoa(record.Month),
			workStudyColDay:     strconv.Itoa(record.Day),
			workStudyColContent: content,
			workStudyColStart:   record.Start,
			workStudyColEnd:     record.End,
			workStudyColHours:   formatWorkStudyHours(record.Hours),
		}
		for key, gridIndex := range columnMap {
			cell, ok := findWorkStudyCellByGrid(row, gridIndex)
			if !ok {
				return nil, fmt.Errorf("\u6a21\u677f\u7b2c %d \u884c\u7f3a\u5c11 %s \u5217", clearStart+index+1, key)
			}
			addWorkStudyCellReplacement(replacements, document, cell, values[key], workStudyTextStyleDefault)
		}
	}
	if totalRowIndex >= 0 {
		cell, ok := findWorkStudyTotalHoursCell(table.Rows[totalRowIndex])
		if !ok {
			return nil, fmt.Errorf("\u6a21\u677f\u5408\u8ba1\u884c\u7f3a\u5c11\u53ef\u5199\u5165\u7684\u603b\u65f6\u6570\u5355\u5143\u683c")
		}
		addWorkStudyCellReplacement(replacements, document, cell, formatWorkStudyTotalHours(records), workStudyTextStyleTotalHours)
	}

	return applyWorkStudyReplacements(document, replacements), nil
}

func parseWorkStudyTables(document []byte) ([]workStudyTable, error) {
	tableRanges, err := findWorkStudyXMLElements(document, "tbl")
	if err != nil {
		return nil, err
	}
	tables := make([]workStudyTable, 0, len(tableRanges))
	for _, tableRange := range tableRanges {
		rows, maxColumns, err := parseWorkStudyRows(document, tableRange)
		if err != nil {
			return nil, err
		}
		tables = append(tables, workStudyTable{Range: tableRange, Rows: rows, MaxColumns: maxColumns})
	}
	return tables, nil
}

func parseWorkStudyRows(document []byte, tableRange workStudyXMLRange) ([]workStudyRow, int, error) {
	tableXML := document[tableRange.Start:tableRange.End]
	rowRanges, err := findWorkStudyXMLElements(tableXML, "tr")
	if err != nil {
		return nil, 0, err
	}

	rows := make([]workStudyRow, 0, len(rowRanges))
	maxColumns := 0
	for _, rowRange := range rowRanges {
		absoluteRowRange := shiftWorkStudyRange(rowRange, tableRange.Start)
		cells, gridLength, err := parseWorkStudyCells(document, absoluteRowRange)
		if err != nil {
			return nil, 0, err
		}
		if gridLength > maxColumns {
			maxColumns = gridLength
		}
		rows = append(rows, workStudyRow{Range: absoluteRowRange, Cells: cells, GridLength: gridLength})
	}
	return rows, maxColumns, nil
}

func parseWorkStudyCells(document []byte, rowRange workStudyXMLRange) ([]workStudyCell, int, error) {
	rowXML := document[rowRange.Start:rowRange.End]
	cellRanges, err := findWorkStudyXMLElements(rowXML, "tc")
	if err != nil {
		return nil, 0, err
	}

	cells := make([]workStudyCell, 0, len(cellRanges))
	gridCursor := 0
	for _, cellRange := range cellRanges {
		absoluteCellRange := shiftWorkStudyRange(cellRange, rowRange.Start)
		cellXML := document[absoluteCellRange.Start:absoluteCellRange.End]
		span := workStudyGridSpan(cellXML)
		cells = append(cells, workStudyCell{
			Range:     absoluteCellRange,
			Text:      extractWorkStudyText(cellXML),
			GridStart: gridCursor,
			GridSpan:  span,
		})
		gridCursor += span
	}
	return cells, gridCursor, nil
}

func detectWorkStudyColumns(table workStudyTable) map[string]int {
	headerTexts := make([]string, table.MaxColumns)
	limit := minInt(3, len(table.Rows))
	for rowIndex := 0; rowIndex < limit; rowIndex++ {
		for _, cell := range table.Rows[rowIndex].Cells {
			for grid := cell.GridStart; grid < cell.GridStart+cell.GridSpan && grid < len(headerTexts); grid++ {
				text := strings.TrimSpace(cell.Text)
				if text == "" {
					continue
				}
				if !strings.Contains(headerTexts[grid], text) {
					headerTexts[grid] += text
				}
			}
		}
	}

	mapping := map[string]int{}
	for index, text := range headerTexts {
		switch {
		case strings.Contains(text, "\u5e74"):
			setFirstWorkStudyColumn(mapping, workStudyColYear, index)
		case strings.Contains(text, "\u6708"):
			setFirstWorkStudyColumn(mapping, workStudyColMonth, index)
		case strings.Contains(text, "\u65e5"):
			setFirstWorkStudyColumn(mapping, workStudyColDay, index)
		case strings.Contains(text, "\u5185\u5bb9") || strings.Contains(text, "\u5730\u70b9"):
			setFirstWorkStudyColumn(mapping, workStudyColContent, index)
		case strings.Contains(text, "\u8d77"):
			setFirstWorkStudyColumn(mapping, workStudyColStart, index)
		case strings.Contains(text, "\u8bab"):
			setFirstWorkStudyColumn(mapping, workStudyColEnd, index)
		case strings.Contains(text, "\u65f6"):
			setFirstWorkStudyColumn(mapping, workStudyColHours, index)
		}
	}
	return mapping
}

func setFirstWorkStudyColumn(mapping map[string]int, key string, index int) {
	if _, ok := mapping[key]; !ok {
		mapping[key] = index
	}
}

func validateWorkStudyColumns(mapping map[string]int) error {
	required := []string{
		workStudyColYear,
		workStudyColMonth,
		workStudyColDay,
		workStudyColContent,
		workStudyColStart,
		workStudyColEnd,
		workStudyColHours,
	}
	missing := []string{}
	for _, key := range required {
		if _, ok := mapping[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("\u6a21\u677f\u5217\u7ed3\u6784\u65e0\u6cd5\u8bc6\u522b\uff1a%s", strings.Join(missing, ", "))
	}
	return nil
}

func findWorkStudyCellByGrid(row workStudyRow, gridIndex int) (workStudyCell, bool) {
	for _, cell := range row.Cells {
		if gridIndex >= cell.GridStart && gridIndex < cell.GridStart+cell.GridSpan {
			return cell, true
		}
	}
	return workStudyCell{}, false
}

func findWorkStudyTotalHoursCell(row workStudyRow) (workStudyCell, bool) {
	totalEnd := -1
	for _, cell := range row.Cells {
		normalized := strings.Join(strings.Fields(cell.Text), "")
		if strings.Contains(normalized, "\u5408\u8ba1") || strings.Contains(cell.Text, "\u5408") {
			totalEnd = maxInt(totalEnd, cell.GridStart+cell.GridSpan)
		}
	}
	if totalEnd < 0 {
		return workStudyCell{}, false
	}

	var candidate workStudyCell
	found := false
	for _, cell := range row.Cells {
		if cell.GridStart >= totalEnd {
			candidate = cell
			found = true
		}
	}
	if found {
		return candidate, true
	}
	if len(row.Cells) == 0 {
		return workStudyCell{}, false
	}
	return row.Cells[len(row.Cells)-1], true
}

func formatWorkStudyTotalHours(records []workStudyRecord) string {
	total := 0.0
	for _, record := range records {
		hours, err := strconv.ParseFloat(strings.TrimSpace(record.Hours), 64)
		if err == nil {
			total += hours
		}
	}
	return formatWorkStudyHours(strconv.FormatFloat(total, 'f', -1, 64)) + "\u5c0f\u65f6"
}

func formatWorkStudyHours(value string) string {
	text := strings.TrimSpace(value)
	hours, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return text
	}
	rounded := math.Round(hours)
	if math.Abs(hours-rounded) < 0.0000001 {
		return strconv.FormatInt(int64(rounded), 10)
	}
	return strconv.FormatFloat(hours, 'f', -1, 64)
}

func addWorkStudyCellReplacement(replacements map[string]workStudyReplacement, document []byte, cell workStudyCell, value string, style workStudyTextStyle) {
	key := fmt.Sprintf("%d:%d", cell.Range.Start, cell.Range.End)
	replacements[key] = workStudyReplacement{
		Start: cell.Range.Start,
		End:   cell.Range.End,
		Text:  replaceWorkStudyCellText(document[cell.Range.Start:cell.Range.End], value, style),
	}
}

func replaceWorkStudyCellText(cellXML []byte, value string, style workStudyTextStyle) []byte {
	startTagEnd := indexByteOrLen(cellXML, '>')
	if startTagEnd >= len(cellXML) {
		return cellXML
	}
	startTagEnd++
	endTagStart := bytes.LastIndex(cellXML, []byte("</"))
	if endTagStart < startTagEnd {
		return cellXML
	}

	prefix := workStudyElementPrefix(cellXML[:startTagEnd], "tc")
	tcPr := []byte{}
	tcPrRanges, err := findWorkStudyXMLElements(cellXML, "tcPr")
	if err == nil && len(tcPrRanges) > 0 && tcPrRanges[0].Start >= startTagEnd && tcPrRanges[0].Start < endTagStart {
		tcPr = cellXML[tcPrRanges[0].Start:tcPrRanges[0].End]
	}

	var buffer bytes.Buffer
	buffer.Write(cellXML[:startTagEnd])
	buffer.Write(tcPr)
	buffer.WriteString(workStudyParagraphXML(prefix, value, style))
	buffer.Write(cellXML[endTagStart:])
	return buffer.Bytes()
}

func workStudyParagraphXML(prefix string, value string, style workStudyTextStyle) string {
	namePrefix := prefix
	attrPrefix := prefix
	escaped := escapeWorkStudyXMLText(value)
	if style != workStudyTextStyleTotalHours {
		return fmt.Sprintf("<%sp><%spPr><%sjc %sval=\"center\"/></%spPr><%sr><%st>%s</%st></%sr></%sp>",
			namePrefix, namePrefix, namePrefix, attrPrefix, namePrefix,
			namePrefix, namePrefix, escaped, namePrefix, namePrefix, namePrefix)
	}
	fontName := "\u6977\u4f53_GB2312"
	return fmt.Sprintf("<%sp><%spPr><%sjc %sval=\"center\"/></%spPr><%sr><%srPr><%srFonts %sascii=\"%s\" %seastAsia=\"%s\" %shint=\"eastAsia\"/><%ssz %sval=\"28\"/></%srPr><%st>%s</%st></%sr></%sp>",
		namePrefix, namePrefix, namePrefix, attrPrefix, namePrefix,
		namePrefix, namePrefix, namePrefix, attrPrefix, fontName, attrPrefix, fontName, attrPrefix, namePrefix, attrPrefix, namePrefix,
		namePrefix, escaped, namePrefix, namePrefix, namePrefix)
}

func clearWorkStudyCoreProperties(content []byte) ([]byte, error) {
	targets := []string{"creator", "lastModifiedBy", "title", "subject", "description", "keywords", "category"}
	replacements := map[string]workStudyReplacement{}
	for _, localName := range targets {
		ranges, err := findWorkStudyXMLElements(content, localName)
		if err != nil {
			return nil, err
		}
		for _, elementRange := range ranges {
			endTagStart := bytes.LastIndex(content[:elementRange.End], []byte("</"))
			if endTagStart < elementRange.StartTagEnd {
				continue
			}
			key := fmt.Sprintf("%d:%d", elementRange.StartTagEnd, endTagStart)
			replacements[key] = workStudyReplacement{
				Start: elementRange.StartTagEnd,
				End:   endTagStart,
				Text:  nil,
			}
		}
	}
	return applyWorkStudyReplacements(content, replacements), nil
}

func applyWorkStudyReplacements(content []byte, replacements map[string]workStudyReplacement) []byte {
	items := make([]workStudyReplacement, 0, len(replacements))
	for _, replacement := range replacements {
		items = append(items, replacement)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Start > items[j].Start
	})

	result := append([]byte(nil), content...)
	for _, replacement := range items {
		if replacement.Start < 0 || replacement.End > len(result) || replacement.Start > replacement.End {
			continue
		}
		next := make([]byte, 0, len(result)-(replacement.End-replacement.Start)+len(replacement.Text))
		next = append(next, result[:replacement.Start]...)
		next = append(next, replacement.Text...)
		next = append(next, result[replacement.End:]...)
		result = next
	}
	return result
}

func findWorkStudyXMLElements(content []byte, localName string) ([]workStudyXMLRange, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	stack := []workStudyXMLRange{}
	ranges := []workStudyXMLRange{}
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
			if typed.Name.Local != localName {
				continue
			}
			startTagEnd := int(decoder.InputOffset())
			start := bytes.LastIndex(content[:startTagEnd], []byte("<"))
			if start < 0 {
				return nil, fmt.Errorf("invalid XML start tag for %s", localName)
			}
			stack = append(stack, workStudyXMLRange{Start: start, StartTagEnd: startTagEnd})
		case xml.EndElement:
			if typed.Name.Local != localName {
				continue
			}
			if len(stack) == 0 {
				return nil, fmt.Errorf("invalid XML element nesting for %s", localName)
			}
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			last.End = int(decoder.InputOffset())
			ranges = append(ranges, last)
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("unclosed XML element %s", localName)
	}
	return ranges, nil
}

func shiftWorkStudyRange(r workStudyXMLRange, offset int) workStudyXMLRange {
	return workStudyXMLRange{
		Start:       r.Start + offset,
		StartTagEnd: r.StartTagEnd + offset,
		End:         r.End + offset,
	}
}

func workStudyGridSpan(cellXML []byte) int {
	decoder := xml.NewDecoder(bytes.NewReader(cellXML))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "gridSpan" {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local != "val" {
				continue
			}
			value, err := strconv.Atoi(strings.TrimSpace(attr.Value))
			if err == nil && value > 0 {
				return value
			}
		}
	}
	return 1
}

func extractWorkStudyText(content []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var builder strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local == "t" {
				inText = true
			}
			if typed.Name.Local == "tab" {
				builder.WriteByte('\t')
			}
		case xml.EndElement:
			if typed.Name.Local == "t" {
				inText = false
			}
		case xml.CharData:
			if inText {
				builder.WriteString(string(typed))
			}
		}
	}
	return strings.TrimSpace(builder.String())
}

func workStudyElementPrefix(startTag []byte, localName string) string {
	text := strings.TrimSpace(string(startTag))
	text = strings.TrimPrefix(text, "<")
	text = strings.TrimPrefix(text, "/")
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	name := strings.TrimSuffix(fields[0], ">")
	suffix := ":" + localName
	if strings.HasSuffix(name, suffix) {
		return strings.TrimSuffix(name, localName)
	}
	return ""
}

func escapeWorkStudyXMLText(value string) string {
	var buffer bytes.Buffer
	if err := xml.EscapeText(&buffer, []byte(value)); err != nil {
		return ""
	}
	return buffer.String()
}

func indexByteOrLen(content []byte, value byte) int {
	index := bytes.IndexByte(content, value)
	if index < 0 {
		return len(content)
	}
	return index
}
