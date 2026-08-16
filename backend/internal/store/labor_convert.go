package store

import (
	"archive/zip"
	"bytes"
	crand "crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"personnel-management-go/internal/types"

	xlsreader "github.com/shakinm/xlsReader/xls"
	"github.com/xuri/excelize/v2"
)

const (
	laborStepCents         int64 = 2500
	laborProxyStepCents    int64 = 5000
	laborMaxPersonCents    int64 = 200000
	laborTaxFreeCents      int64 = 80000
	laborProxyHardCapCents int64 = 190000
	laborNoiseMinCents     int64 = 5000
	laborNoiseMaxCents     int64 = 10000
	laborTeamFundSource          = "\u56e2\u961f\u7ecf\u8d39"
)

var laborHistoryIDPattern = regexp.MustCompile(`^[a-f0-9-]{36}$`)

type laborPerson struct {
	Name           string
	Original       int64
	DutyHours      float64
	WorkOrderHours float64
	Management     int64
	Adjusted       int64
	Remarks        []string
}

type laborSource struct {
	Name   string
	Amount int64
}

type laborReceiver struct {
	Name   string
	Amount int64
}

type laborNoiseItem struct {
	Name      string
	Reduction int64
}

type laborNoise struct {
	Applied bool
	Items   []laborNoiseItem
}

type laborTransfer struct {
	Source   string
	Receiver string
	Amount   int64
}

type laborAdjustmentResult struct {
	People        []laborPerson
	TargetTotal   int64
	OriginalTotal int64
	BaseTotal     int64
	FinalTotal    int64
	TeamFund      int64
	Warnings      []string
	Noise         laborNoise
	Transfers     []laborTransfer
}

type laborRunOptions struct {
	InputFilename        string
	OutputName           string
	CSVName              string
	CSVOutputMonth       string
	SourceFinanceBatchID string
	ParentRunID          string
	IsManualAdjust       bool
}

func ParseLaborMoneyToCents(value string) (int64, error) {
	text := strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	text = strings.TrimPrefix(text, "¥")
	text = strings.TrimPrefix(text, "￥")
	text = strings.TrimPrefix(text, "楼")
	if text == "" {
		return 0, fmt.Errorf("金额不能为空")
	}
	amount, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("金额格式无效：%s", value)
	}
	return int64(math.Round(amount * 100)), nil
}

func (s *Store) ConvertLaborWorkbook(content []byte, inputFilename string, targetTotal int64, seed *int64, csvOutputMonth string) (types.LaborConvertResponse, error) {
	return s.convertLaborContent(content, inputFilename, targetTotal, seed, laborRunOptions{
		InputFilename:  inputFilename,
		CSVOutputMonth: normalizeLaborCSVOutputMonth(csvOutputMonth, inputFilename),
	})
}

func (s *Store) ConvertLaborFinanceBatch(batchID string, targetTotal int64, seed *int64) (types.LaborConvertResponse, error) {
	batch, content, err := s.GetFinanceLocalBatchWorkbook(batchID)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}
	return s.convertLaborContent(content, batch.ExcelFilename, targetTotal, seed, laborRunOptions{
		InputFilename:        batch.ExcelFilename,
		CSVOutputMonth:       normalizeLaborCSVOutputMonth(batch.OutputMonth, batch.ExcelFilename),
		SourceFinanceBatchID: batch.ID,
	})
}

func (s *Store) convertLaborContent(content []byte, inputFilename string, targetTotal int64, seed *int64, options laborRunOptions) (types.LaborConvertResponse, error) {
	if targetTotal <= 0 {
		return types.LaborConvertResponse{}, fmt.Errorf("目标总额必须大于 0")
	}
	effectiveSeed := seed
	if effectiveSeed == nil {
		effectiveSeed = s.cfg.LaborSeed
	}
	people, err := readLaborPeopleFromUploadedFile(content, inputFilename)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}

	result, err := adjustLabor(people, targetTotal, effectiveSeed)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}

	rolesByRealName, err := s.getLaborRolesByRealName()
	if err != nil {
		return types.LaborConvertResponse{}, err
	}

	id, err := newLaborRunID()
	if err != nil {
		return types.LaborConvertResponse{}, err
	}
	createdAt := time.Now().Format("2006-01-02 15:04:05")
	if strings.TrimSpace(options.InputFilename) == "" {
		options.InputFilename = inputFilename
	}
	if strings.TrimSpace(options.OutputName) == "" {
		options.OutputName = fmt.Sprintf("%s-调整后劳务计算.xlsx", safeLaborStem(inputFilename))
	}
	if strings.TrimSpace(options.CSVName) == "" {
		options.CSVName = fmt.Sprintf("%s-调整后劳务计算.csv", safeLaborStem(inputFilename))
	}
	options.CSVOutputMonth = normalizeLaborCSVOutputMonth(options.CSVOutputMonth, inputFilename)

	response, workbook, csvContent, err := s.buildAndPersistLaborRun(id, createdAt, effectiveSeed, result, options, rolesByRealName)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}

	if err := s.saveLaborConversionRun(response, result, workbook, csvContent); err != nil {
		return types.LaborConvertResponse{}, err
	}
	return response, nil
}

func (s *Store) ListLaborConversionRuns() ([]types.LaborConvertHistoryItem, error) {
	rows, err := s.db.Query(`
		SELECT id, created_at, input_filename, output_name, csv_name, csv_output_month, target_total_cents, final_total_cents,
			COALESCE(length(csv_blob), 0), people_json, source_finance_batch_id, is_manual_adjust
		FROM labor_conversion_runs
		ORDER BY created_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]types.LaborConvertHistoryItem, 0)
	for rows.Next() {
		var item types.LaborConvertHistoryItem
		var targetTotal int64
		var finalTotal int64
		var csvSize int
		var peopleJSON string
		var isManualAdjust int
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.InputFilename, &item.OutputName, &item.CSVName, &item.CSVOutputMonth, &targetTotal, &finalTotal, &csvSize, &peopleJSON, &item.SourceFinanceBatchID, &isManualAdjust); err != nil {
			return nil, err
		}
		item.TargetTotal = formatLaborMoney(targetTotal)
		item.FinalTotal = formatLaborMoney(finalTotal)
		item.DownloadURL = fmt.Sprintf("/api/labor-convert/history/%s/download", item.ID)
		item.HasCSV = csvSize > 0
		if item.HasCSV {
			item.CSVDownloadURL = fmt.Sprintf("/api/labor-convert/history/%s/download/csv", item.ID)
		}
		item.CanManualAdjust = strings.TrimSpace(peopleJSON) != ""
		item.IsManualAdjust = isManualAdjust != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

// DeleteLaborConversionRun removes one persisted history snapshot, including
// its result JSON and workbook/CSV blobs. Source finance batches are stored in
// a separate table and are intentionally left untouched.
func (s *Store) DeleteLaborConversionRun(id string) error {
	if !laborHistoryIDPattern.MatchString(id) {
		return sql.ErrNoRows
	}
	result, err := s.db.Exec("DELETE FROM labor_conversion_runs WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) GetLaborConversionRun(id string) (types.LaborConvertResponse, error) {
	if !laborHistoryIDPattern.MatchString(id) {
		return types.LaborConvertResponse{}, sql.ErrNoRows
	}
	var payload string
	var csvName string
	var csvSize int
	var csvOutputMonth string
	var sourceFinanceBatchID string
	var parentRunID string
	var peopleJSON string
	var isManualAdjust int
	err := s.db.QueryRow(`
		SELECT result_json, csv_name, COALESCE(length(csv_blob), 0), csv_output_month,
			source_finance_batch_id, parent_run_id, people_json, is_manual_adjust
		FROM labor_conversion_runs
		WHERE id = ?
	`, id).Scan(&payload, &csvName, &csvSize, &csvOutputMonth, &sourceFinanceBatchID, &parentRunID, &peopleJSON, &isManualAdjust)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}

	var response types.LaborConvertResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return types.LaborConvertResponse{}, err
	}
	response.HasCSV = csvSize > 0
	response.CSVName = csvName
	response.CSVOutputMonth = csvOutputMonth
	response.SourceFinanceBatchID = sourceFinanceBatchID
	response.ParentRunID = parentRunID
	response.IsManualAdjust = isManualAdjust != 0
	response.CanManualAdjust = strings.TrimSpace(peopleJSON) != ""
	response.DownloadURL = fmt.Sprintf("/api/labor-convert/history/%s/download", id)
	if response.HasCSV {
		response.CSVDownloadURL = fmt.Sprintf("/api/labor-convert/history/%s/download/csv", id)
	}
	return response, nil
}

func (s *Store) GetLaborConversionWorkbook(id string) (string, []byte, error) {
	if !laborHistoryIDPattern.MatchString(id) {
		return "", nil, sql.ErrNoRows
	}
	var filename string
	var content []byte
	var csvOutputMonth string
	var peoplePayload string
	err := s.db.QueryRow(`
		SELECT output_name, workbook_blob, csv_output_month, people_json
		FROM labor_conversion_runs
		WHERE id = ?
	`, id).Scan(&filename, &content, &csvOutputMonth, &peoplePayload)
	if err != nil {
		return "", nil, err
	}
	if normalized := laborMonthFilenamePrefix(csvOutputMonth); normalized != "" {
		filename = normalized + "\u52b3\u52a1\u8ba1\u7b97.xlsx"
	}
	if strings.TrimSpace(peoplePayload) != "" {
		var people []laborPerson
		if err := json.Unmarshal([]byte(peoplePayload), &people); err != nil {
			return "", nil, err
		}
		rolesByRealName, err := s.getLaborRolesByRealName()
		if err != nil {
			return "", nil, err
		}
		content, err = createLaborCalculationWorkbook(laborAdjustmentResult{People: people}, rolesByRealName, s.rates)
		if err != nil {
			return "", nil, err
		}
	}
	return filename, content, nil
}

func (s *Store) GetLaborWorkStudyConversionWorkbook(id string) (string, []byte, error) {
	if !laborHistoryIDPattern.MatchString(id) {
		return "", nil, sql.ErrNoRows
	}
	var peoplePayload string
	var csvOutputMonth string
	err := s.db.QueryRow(`
		SELECT people_json, csv_output_month
		FROM labor_conversion_runs
		WHERE id = ?
	`, id).Scan(&peoplePayload, &csvOutputMonth)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(peoplePayload) == "" {
		return "", nil, sql.ErrNoRows
	}
	var people []laborPerson
	if err := json.Unmarshal([]byte(peoplePayload), &people); err != nil {
		return "", nil, err
	}
	content, err := createLaborWorkStudyConversionWorkbook(people, csvOutputMonth)
	if err != nil {
		return "", nil, err
	}
	filenamePrefix := laborMonthFilenamePrefix(csvOutputMonth)
	if filenamePrefix == "" {
		filenamePrefix = time.Now().Format("2006\u5e7401\u6708")
	}
	return filenamePrefix + "\u52b3\u52a1\u52e4\u52a9\u8f6c\u6362.xlsx", content, nil
}

func (s *Store) GetLaborConversionCSV(id string) (string, []byte, error) {
	if !laborHistoryIDPattern.MatchString(id) {
		return "", nil, sql.ErrNoRows
	}
	var filename string
	var content []byte
	err := s.db.QueryRow(`
		SELECT csv_name, csv_blob
		FROM labor_conversion_runs
		WHERE id = ? AND csv_blob IS NOT NULL
	`, id).Scan(&filename, &content)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(filename) == "" {
		filename = "labor-convert.csv"
	}
	return filename, content, nil
}

func (s *Store) ManualAdjustLaborConversionRun(id string, request types.LaborManualAdjustRequest) (types.LaborConvertResponse, error) {
	if !laborHistoryIDPattern.MatchString(id) {
		return types.LaborConvertResponse{}, sql.ErrNoRows
	}

	var inputFilename string
	var targetTotal int64
	var seed sql.NullInt64
	var csvOutputMonth string
	var sourceFinanceBatchID string
	var peoplePayload string
	err := s.db.QueryRow(`
		SELECT input_filename, target_total_cents, seed, csv_output_month, source_finance_batch_id, people_json
		FROM labor_conversion_runs
		WHERE id = ?
	`, id).Scan(&inputFilename, &targetTotal, &seed, &csvOutputMonth, &sourceFinanceBatchID, &peoplePayload)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}
	if strings.TrimSpace(peoplePayload) == "" {
		return types.LaborConvertResponse{}, fmt.Errorf("该历史记录不支持手动调额，请重新生成后再调整")
	}

	var people []laborPerson
	if err := json.Unmarshal([]byte(peoplePayload), &people); err != nil {
		return types.LaborConvertResponse{}, err
	}
	adjustedByName := map[string]int64{}
	for _, row := range request.Rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			return types.LaborConvertResponse{}, fmt.Errorf("调额人员姓名不能为空")
		}
		if _, exists := adjustedByName[name]; exists {
			return types.LaborConvertResponse{}, fmt.Errorf("%s 出现了重复调额记录", name)
		}
		amount, err := ParseLaborMoneyToCents(row.Adjusted)
		if err != nil {
			return types.LaborConvertResponse{}, err
		}
		if amount < 0 {
			return types.LaborConvertResponse{}, fmt.Errorf("%s 调整后金额不能为负数", name)
		}
		if amount%laborStepCents != 0 {
			return types.LaborConvertResponse{}, fmt.Errorf("%s 调整后金额必须是 25 元的整数倍", name)
		}
		if amount > laborMaxPersonCents {
			return types.LaborConvertResponse{}, fmt.Errorf("%s 调整后金额不能超过 2000 元", name)
		}
		adjustedByName[name] = amount
	}
	if len(adjustedByName) != len(people) {
		return types.LaborConvertResponse{}, fmt.Errorf("调额人员名单必须与原结果完全一致")
	}

	var finalTotal int64
	for i := range people {
		amount, ok := adjustedByName[people[i].Name]
		if !ok {
			return types.LaborConvertResponse{}, fmt.Errorf("缺少 %s 的调整后金额", people[i].Name)
		}
		people[i].Adjusted = amount
		people[i].Remarks = nil
		finalTotal += amount
	}
	if finalTotal != targetTotal {
		return types.LaborConvertResponse{}, fmt.Errorf("手动调整后合计 %s 必须等于目标总额 %s", formatLaborMoney(finalTotal), formatLaborMoney(targetTotal))
	}

	originalTotal := sumLaborOriginal(people)
	transfers := buildLaborTransferPlan(people, targetTotal, originalTotal)
	applyLaborTransferRemarks(people, transfers)
	result := laborAdjustmentResult{
		People:        people,
		TargetTotal:   targetTotal,
		OriginalTotal: originalTotal,
		BaseTotal:     originalTotal,
		FinalTotal:    finalTotal,
		TeamFund:      targetTotal - originalTotal,
		Warnings:      []string{"该记录由手动调额保存生成"},
		Transfers:     transfers,
	}

	rolesByRealName, err := s.getLaborRolesByRealName()
	if err != nil {
		return types.LaborConvertResponse{}, err
	}
	newID, err := newLaborRunID()
	if err != nil {
		return types.LaborConvertResponse{}, err
	}
	createdAt := time.Now().Format("2006-01-02 15:04:05")
	var effectiveSeed *int64
	if seed.Valid {
		effectiveSeed = &seed.Int64
	}
	options := laborRunOptions{
		InputFilename:        inputFilename,
		OutputName:           fmt.Sprintf("%s-手动调整后劳务计算.xlsx", safeLaborStem(inputFilename)),
		CSVName:              fmt.Sprintf("%s-手动调整后劳务计算.csv", safeLaborStem(inputFilename)),
		CSVOutputMonth:       normalizeLaborCSVOutputMonth(csvOutputMonth, inputFilename),
		SourceFinanceBatchID: sourceFinanceBatchID,
		ParentRunID:          id,
		IsManualAdjust:       true,
	}

	response, workbook, csvContent, err := s.buildAndPersistLaborRun(newID, createdAt, effectiveSeed, result, options, rolesByRealName)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}
	if err := s.saveLaborConversionRun(response, result, workbook, csvContent); err != nil {
		return types.LaborConvertResponse{}, err
	}
	return response, nil
}

func (s *Store) getLaborRolesByRealName() (map[string]string, error) {
	rows, err := s.db.Query(`
		SELECT real_name, role
		FROM users
		WHERE is_active = 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rolesByRealName := map[string]string{}
	for rows.Next() {
		var realName string
		var role string
		if err := rows.Scan(&realName, &role); err != nil {
			return nil, err
		}
		rolesByRealName[strings.TrimSpace(realName)] = strings.TrimSpace(role)
	}
	return rolesByRealName, rows.Err()
}

func (s *Store) buildAndPersistLaborRun(id string, createdAt string, seed *int64, result laborAdjustmentResult, options laborRunOptions, rolesByRealName map[string]string) (types.LaborConvertResponse, []byte, []byte, error) {
	workbook, err := createLaborCalculationWorkbook(result, rolesByRealName, s.rates)
	if err != nil {
		return types.LaborConvertResponse{}, nil, nil, err
	}
	csvContent, err := s.createLaborAdjustedCSV(result, options)
	if err != nil {
		return types.LaborConvertResponse{}, nil, nil, err
	}

	response := buildLaborResponse(id, createdAt, seed, result, options)
	if err := s.writeLaborRunFiles(response, workbook, csvContent); err != nil {
		return types.LaborConvertResponse{}, nil, nil, err
	}
	return response, workbook, csvContent, nil
}

func (s *Store) writeLaborRunFiles(response types.LaborConvertResponse, workbook []byte, csvContent []byte) error {
	return nil
}

func (s *Store) saveLaborConversionRun(response types.LaborConvertResponse, result laborAdjustmentResult, workbook []byte, csvContent []byte) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	peoplePayload, err := json.Marshal(result.People)
	if err != nil {
		return err
	}

	var seed any
	if response.Seed != nil {
		seed = *response.Seed
	}

	_, err = s.db.Exec(`
		INSERT INTO labor_conversion_runs
			(id, created_at, input_filename, output_name, csv_name, target_total_cents, original_total_cents, final_total_cents,
			 team_fund_cents, seed, csv_output_month, source_finance_batch_id, local_output_dir, parent_run_id, is_manual_adjust,
			 people_json, result_json, workbook_blob, csv_blob)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, response.HistoryID, response.CreatedAt, response.InputFilename, response.OutputName, response.CSVName, result.TargetTotal,
		result.OriginalTotal, result.FinalTotal, result.TeamFund, seed, response.CSVOutputMonth, response.SourceFinanceBatchID,
		"database:"+response.HistoryID, response.ParentRunID, boolToInt(response.IsManualAdjust), string(peoplePayload), string(payload), workbook, csvContent)
	return err
}

func readLaborPeopleFromUploadedFile(content []byte, inputFilename string) ([]laborPerson, error) {
	switch strings.ToLower(filepath.Ext(inputFilename)) {
	case ".xlsx":
		if err := ensureXLSXHasNoMacros(content); err != nil {
			return nil, err
		}
		return readLaborPeopleFromXLSX(content)
	case ".xls":
		return readLaborPeopleFromXLS(content)
	case ".csv":
		return readLaborPeopleFromCSV(content)
	default:
		return nil, fmt.Errorf("only .xlsx, .xls, or .csv files are supported")
	}
}

func readLaborPeopleFromXLSX(content []byte) ([]laborPerson, error) {
	workbook, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("unable to read xlsx file: %w", err)
	}
	defer workbook.Close()

	errorsBySheet := make([]string, 0)
	for _, sheet := range workbook.GetSheetList() {
		rows, err := workbook.GetRows(sheet)
		if err != nil {
			errorsBySheet = append(errorsBySheet, fmt.Sprintf("%s: %s", sheet, err.Error()))
			continue
		}
		people, err := readLaborPeopleFromRows(rows)
		if err == nil && len(people) > 0 {
			return people, nil
		}
		if err != nil {
			errorsBySheet = append(errorsBySheet, fmt.Sprintf("%s: %s", sheet, err.Error()))
		}
	}

	if len(errorsBySheet) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errorsBySheet, "; "))
	}
	return nil, fmt.Errorf("no sheet contains recognizable name and amount columns")
}

func ensureXLSXHasNoMacros(content []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return fmt.Errorf("unable to read xlsx file: %w", err)
	}

	for _, file := range reader.File {
		name := strings.ToLower(file.Name)
		if name == "xl/vbaproject.bin" || strings.Contains(name, "vbaproject") {
			return fmt.Errorf("macro-enabled Excel files are not supported")
		}
		if name != "[content_types].xml" {
			continue
		}

		handle, err := file.Open()
		if err != nil {
			return fmt.Errorf("unable to inspect xlsx content types: %w", err)
		}
		data, readErr := io.ReadAll(handle)
		closeErr := handle.Close()
		if readErr != nil {
			return fmt.Errorf("unable to inspect xlsx content types: %w", readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("unable to inspect xlsx content types: %w", closeErr)
		}

		contentTypes := strings.ToLower(string(data))
		if strings.Contains(contentTypes, "macroenabled") || strings.Contains(contentTypes, "vbaproject") {
			return fmt.Errorf("macro-enabled Excel files are not supported")
		}
	}
	return nil
}

func readLaborPeopleFromXLS(content []byte) ([]laborPerson, error) {
	workbook, err := xlsreader.OpenReader(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("unable to read xls file: %w", err)
	}

	errorsBySheet := make([]string, 0)
	for i := 0; i < workbook.GetNumberSheets(); i++ {
		sheet, err := workbook.GetSheet(i)
		if err != nil {
			errorsBySheet = append(errorsBySheet, fmt.Sprintf("sheet %d: %s", i+1, err.Error()))
			continue
		}
		people, err := readLaborPeopleFromRows(xlsSheetRows(sheet))
		if err == nil && len(people) > 0 {
			return people, nil
		}
		if err != nil {
			errorsBySheet = append(errorsBySheet, fmt.Sprintf("%s: %s", sheet.GetName(), err.Error()))
		}
	}

	if len(errorsBySheet) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errorsBySheet, "; "))
	}
	return nil, fmt.Errorf("no sheet contains recognizable name and amount columns")
}

func xlsSheetRows(sheet *xlsreader.Sheet) [][]string {
	rows := make([][]string, 0, sheet.GetNumberRows())
	for i := 0; i < sheet.GetNumberRows(); i++ {
		row, err := sheet.GetRow(i)
		if err != nil {
			continue
		}
		cells := row.GetCols()
		values := make([]string, len(cells))
		for j, cell := range cells {
			values[j] = strings.TrimSpace(cell.GetString())
		}
		rows = append(rows, values)
	}
	return rows
}

func readLaborPeopleFromCSV(content []byte) ([]laborPerson, error) {
	reader := csv.NewReader(bytes.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("unable to read csv file: %w", err)
	}
	for i := range rows {
		for j := range rows[i] {
			rows[i][j] = strings.TrimSpace(rows[i][j])
		}
	}
	if len(rows) > 0 && len(rows[0]) > 0 {
		rows[0][0] = strings.TrimPrefix(rows[0][0], "\ufeff")
	}
	return readLaborPeopleFromRows(rows)
}
func readLaborPeopleFromRows(rows [][]string) ([]laborPerson, error) {
	headerIndex := -1
	nameCol := -1
	amountCol := -1
	dutyCol := -1
	workOrderCol := -1
	managementCol := -1

	for index, row := range rows {
		labels := make([]string, len(row))
		for i, value := range row {
			labels[i] = normalizeLaborHeader(value)
		}
		maybeName := findLaborHeader(labels, []string{"姓名", "名字", "成员", "name"})
		maybeAmount := findLaborAmountHeader(labels)
		if maybeName >= 0 && maybeAmount >= 0 {
			headerIndex = index
			nameCol = maybeName
			amountCol = maybeAmount
			dutyCol = findLaborHeader(labels, []string{"值班时长", "值班工时", "duty"})
			workOrderCol = findLaborHeader(labels, []string{"工单时长", "工单工时", "workorder", "work order"})
			managementCol = findLaborHeader(labels, []string{"项目管理薪酬", "项目管理费用", "管理费用", "management"})
			break
		}
	}

	if headerIndex < 0 || nameCol < 0 || amountCol < 0 {
		return nil, fmt.Errorf("name or amount column was not found")
	}

	byName := map[string]*laborPerson{}
	order := make([]string, 0)
	for _, row := range rows[headerIndex+1:] {
		if nameCol >= len(row) || amountCol >= len(row) {
			continue
		}
		name := strings.TrimSpace(row[nameCol])
		if name == "" || isLaborSummaryRow(name) {
			continue
		}
		amount, err := laborCellToCents(row[amountCol])
		if err != nil {
			continue
		}

		person, ok := byName[name]
		if !ok {
			person = &laborPerson{Name: name}
			byName[name] = person
			order = append(order, name)
		}
		person.Original += amount
		person.DutyHours += laborCellToFloat(row, dutyCol)
		person.WorkOrderHours += laborCellToFloat(row, workOrderCol)
		person.Management += laborCellToCentsOrZero(row, managementCol)
	}

	people := make([]laborPerson, 0, len(order))
	for _, name := range order {
		people = append(people, *byName[name])
	}
	return people, nil
}

func adjustLabor(people []laborPerson, targetTotal int64, seed *int64) (laborAdjustmentResult, error) {
	if len(people) == 0 {
		return laborAdjustmentResult{}, fmt.Errorf("spreadsheet does not contain calculable people rows")
	}
	if targetTotal%laborStepCents != 0 {
		return laborAdjustmentResult{}, fmt.Errorf("target total must be a multiple of 25")
	}

	adjustedPeople := make([]laborPerson, len(people))
	for i, person := range people {
		if person.Original%laborStepCents != 0 {
			return laborAdjustmentResult{}, fmt.Errorf("%s original amount is not a multiple of 25", person.Name)
		}
		person.Adjusted = minInt64(person.Original, laborProxyHardCapCents)
		person.Remarks = nil
		adjustedPeople[i] = person
	}

	rngSeed := time.Now().UnixNano()
	if seed != nil {
		rngSeed = *seed
	}
	rng := mrand.New(mrand.NewSource(rngSeed))

	originalTotal := sumLaborOriginal(adjustedPeople)
	baseTotal := sumLaborAdjusted(adjustedPeople)
	maxTotal := int64(len(adjustedPeople)) * laborMaxPersonCents
	if targetTotal > maxTotal {
		return laborAdjustmentResult{}, fmt.Errorf("目标总额 %s 超过当前人员可承载上限 %s，请降低目标总额或增加可代发人员", formatLaborMoney(targetTotal), formatLaborMoney(maxTotal))
	}
	warnings := []string{}
	if targetTotal >= baseTotal {
		remaining := targetTotal - baseTotal
		remaining = allocateLaborSurplus(adjustedPeople, remaining, map[string]struct{}{}, rng)
		if remaining > 0 {
			warnings = append(warnings, "priority quota was insufficient; filled up to the 2000 monthly cap")
			remaining = fillLaborProxyToCap(laborPeopleRefs(adjustedPeople, nil), remaining, laborMaxPersonCents, map[string]struct{}{}, rng)
		}
		if remaining > 0 {
			return laborAdjustmentResult{}, fmt.Errorf("%s cannot be allocated within the 2000 monthly cap", formatLaborMoney(remaining))
		}
	} else {
		warnings = append(warnings, "target total is below the baseline; high-amount members were reduced first")
		if err := reduceLaborTotal(adjustedPeople, baseTotal-targetTotal); err != nil {
			return laborAdjustmentResult{}, err
		}
	}

	noise := applyLaborNoiseIfNeeded(adjustedPeople, rng, &warnings)
	finalTotal := sumLaborAdjusted(adjustedPeople)
	if finalTotal != targetTotal {
		delta := targetTotal - finalTotal
		if delta > 0 {
			leftover := allocateLaborSurplus(adjustedPeople, delta, map[string]struct{}{}, rng)
			if leftover > 0 {
				leftover = fillLaborProxyToCap(laborPeopleRefs(adjustedPeople, nil), leftover, laborMaxPersonCents, map[string]struct{}{}, rng)
			}
			if leftover > 0 {
				return laborAdjustmentResult{}, fmt.Errorf("%s could not be reallocated after random reduction", formatLaborMoney(leftover))
			}
		} else if err := reduceLaborTotal(adjustedPeople, -delta); err != nil {
			return laborAdjustmentResult{}, err
		}
	}

	applyZeroLaborHelperVariation(adjustedPeople, rng)
	transfers := buildLaborTransferPlan(adjustedPeople, targetTotal, originalTotal)
	applyLaborTransferRemarks(adjustedPeople, transfers)

	return laborAdjustmentResult{
		People:        adjustedPeople,
		TargetTotal:   targetTotal,
		OriginalTotal: originalTotal,
		BaseTotal:     baseTotal,
		FinalTotal:    sumLaborAdjusted(adjustedPeople),
		TeamFund:      targetTotal - originalTotal,
		Warnings:      warnings,
		Noise:         noise,
		Transfers:     transfers,
	}, nil
}

func allocateLaborSurplus(people []laborPerson, amount int64, excluded map[string]struct{}, rng *mrand.Rand) int64 {
	if amount <= 0 {
		return amount
	}
	zeroOriginals := laborPeopleRefs(people, func(person laborPerson) bool { return person.Original == 0 })
	lowOriginals := laborPeopleRefs(people, func(person laborPerson) bool {
		return person.Original > 0 && person.Original < laborTaxFreeCents
	})

	amount = fillLaborProxyToCap(zeroOriginals, amount, laborTaxFreeCents, excluded, rng)
	amount = fillLaborProxyToCap(lowOriginals, amount, laborTaxFreeCents, excluded, rng)
	amount = fillLaborProxyToCap(laborPeopleRefs(people, nil), amount, laborProxyHardCapCents, excluded, rng)
	return amount
}

func fillLaborProxyToCap(people []*laborPerson, amount int64, cap int64, excluded map[string]struct{}, rng *mrand.Rand) int64 {
	amount = randomLaborFillToCap(people, amount, cap, laborProxyStepCents, excluded, rng)
	if amount == laborStepCents {
		return allocateSingleLaborStepToCap(people, amount, cap, excluded, rng)
	}
	return amount
}

func allocateSingleLaborStepToCap(people []*laborPerson, amount int64, cap int64, excluded map[string]struct{}, rng *mrand.Rand) int64 {
	if amount != laborStepCents {
		return amount
	}
	candidates := make([]int, 0)
	for i := range people {
		if _, ok := excluded[people[i].Name]; ok || people[i].Adjusted+laborStepCents > cap {
			continue
		}
		candidates = append(candidates, i)
	}
	if len(candidates) == 0 {
		return amount
	}
	rng.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	sort.SliceStable(candidates, func(i, j int) bool {
		left := people[candidates[i]]
		right := people[candidates[j]]
		return left.Adjusted < right.Adjusted
	})
	windowSize := minInt(len(candidates), 4)
	pick := candidates[rng.Intn(windowSize)]
	people[pick].Adjusted += laborStepCents
	return 0
}

func randomLaborFillToCap(people []*laborPerson, amount int64, cap int64, step int64, excluded map[string]struct{}, rng *mrand.Rand) int64 {
	if amount <= 0 {
		return amount
	}
	if step <= 0 {
		step = laborStepCents
	}
	eligible := make([]int, 0)
	var totalCapacity int64
	for i := range people {
		if _, ok := excluded[people[i].Name]; ok || people[i].Adjusted >= cap {
			continue
		}
		eligible = append(eligible, i)
		totalCapacity += ((cap - people[i].Adjusted) / step) * step
	}
	if len(eligible) == 0 || totalCapacity <= 0 {
		return amount
	}
	if amount >= totalCapacity {
		for _, index := range eligible {
			people[index].Adjusted += ((cap - people[index].Adjusted) / step) * step
		}
		return amount - totalCapacity
	}

	stepsLeft := amount / step
	for stepsLeft > 0 {
		candidates := make([]int, 0)
		for _, index := range eligible {
			if people[index].Adjusted+step <= cap {
				candidates = append(candidates, index)
			}
		}
		if len(candidates) == 0 {
			break
		}
		rng.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
		sort.SliceStable(candidates, func(i, j int) bool {
			left := people[candidates[i]]
			right := people[candidates[j]]
			return left.Adjusted < right.Adjusted
		})
		windowSize := minInt(len(candidates), 4)
		pick := candidates[rng.Intn(windowSize)]
		capacitySteps := (cap - people[pick].Adjusted) / step
		chunkSteps := int64(rng.Intn(int(minInt64(capacitySteps, minInt64(stepsLeft, 4)))) + 1)
		people[pick].Adjusted += chunkSteps * step
		stepsLeft -= chunkSteps
	}
	return amount%step + stepsLeft*step
}

func reduceLaborTotal(people []laborPerson, amount int64) error {
	if amount%laborStepCents != 0 {
		return fmt.Errorf("reduction amount must be a multiple of 25")
	}
	indices := make([]int, len(people))
	for i := range people {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		left := people[indices[i]]
		right := people[indices[j]]
		if left.Adjusted == right.Adjusted {
			return left.Name > right.Name
		}
		return left.Adjusted > right.Adjusted
	})
	for _, index := range indices {
		if amount <= 0 {
			return nil
		}
		reducible := minInt64((people[index].Adjusted/laborStepCents)*laborStepCents, amount)
		people[index].Adjusted -= reducible
		amount -= reducible
	}
	if amount > 0 {
		return fmt.Errorf("target total is too low to reduce to")
	}
	return nil
}

func applyLaborNoiseIfNeeded(people []laborPerson, rng *mrand.Rand, warnings *[]string) laborNoise {
	atCap := make([]int, 0)
	for i, person := range people {
		if person.Adjusted == laborMaxPersonCents {
			atCap = append(atCap, i)
		}
	}
	if len(atCap) <= 1 {
		return laborNoise{}
	}
	rng.Shuffle(len(atCap), func(i, j int) { atCap[i], atCap[j] = atCap[j], atCap[i] })
	selected := atCap[:len(atCap)-1]
	selectedSet := map[string]struct{}{}
	for _, index := range selected {
		selectedSet[people[index].Name] = struct{}{}
	}

	var capacity int64
	for _, person := range people {
		if _, ok := selectedSet[person.Name]; !ok {
			capacity += maxInt64(0, laborMaxPersonCents-person.Adjusted)
		}
	}
	if capacity < laborNoiseMinCents*int64(len(selected)) {
		*warnings = append(*warnings, "multiple members reached 2000, but there was not enough capacity for random reduction")
		return laborNoise{}
	}

	choices := []int64{laborNoiseMinCents, laborNoiseMaxCents}
	reductions := make([]laborNoiseItem, 0, len(selected))
	remainingCapacity := capacity
	var freed int64
	for i, index := range selected {
		peopleLeft := int64(len(selected) - i - 1)
		maxAllowed := remainingCapacity - peopleLeft*laborNoiseMinCents
		available := make([]int64, 0, len(choices))
		for _, choice := range choices {
			if choice <= maxAllowed {
				available = append(available, choice)
			}
		}
		amount := laborNoiseMinCents
		if len(available) > 0 {
			amount = available[rng.Intn(len(available))]
		}
		people[index].Adjusted -= amount
		remainingCapacity -= amount
		freed += amount
		reductions = append(reductions, laborNoiseItem{Name: people[index].Name, Reduction: amount})
	}

	leftover := allocateLaborSurplus(people, freed, selectedSet, rng)
	if leftover > 0 {
		leftover = fillLaborProxyToCap(laborPeopleRefs(people, nil), leftover, laborMaxPersonCents, selectedSet, rng)
	}
	if leftover > 0 {
		*warnings = append(*warnings, fmt.Sprintf("%s released by random reduction could not be fully reallocated", formatLaborMoney(leftover)))
	}
	return laborNoise{Applied: true, Items: reductions}
}

func applyZeroLaborHelperVariation(people []laborPerson, rng *mrand.Rand) {
	zeroHelpers := make([]int, 0)
	values := map[int64]struct{}{}
	for i, person := range people {
		if person.Original == 0 && person.Adjusted > 0 {
			zeroHelpers = append(zeroHelpers, i)
			values[person.Adjusted] = struct{}{}
		}
	}
	if len(zeroHelpers) <= 1 || len(values) > 1 {
		return
	}

	recipients := laborPeopleRefs(people, func(person laborPerson) bool {
		return person.Original > 0 && person.Adjusted < laborTaxFreeCents
	})
	extraRecipients := laborPeopleRefs(people, func(person laborPerson) bool {
		return person.Original > 0 && person.Adjusted < laborProxyHardCapCents
	})
	capacity := laborCapacity(recipients, laborTaxFreeCents, laborProxyStepCents)
	if capacity < laborProxyStepCents {
		capacity = laborCapacity(extraRecipients, laborProxyHardCapCents, laborProxyStepCents)
	}
	if capacity < laborProxyStepCents {
		return
	}

	rng.Shuffle(len(zeroHelpers), func(i, j int) { zeroHelpers[i], zeroHelpers[j] = zeroHelpers[j], zeroHelpers[i] })
	selectedCount := maxInt(1, len(zeroHelpers)-1)
	choices := []int64{laborProxyStepCents, laborProxyStepCents * 2, laborProxyStepCents * 3, laborProxyStepCents * 4}
	type reduction struct {
		Index  int
		Amount int64
	}
	reductions := []reduction{}
	var freed int64
	for _, index := range zeroHelpers[:selectedCount] {
		maxReduction := minInt64(people[index].Adjusted, choices[len(choices)-1])
		available := make([]int64, 0, len(choices))
		for _, choice := range choices {
			if choice <= maxReduction && freed+choice <= capacity {
				available = append(available, choice)
			}
		}
		if len(available) == 0 {
			continue
		}
		amount := available[rng.Intn(len(available))]
		people[index].Adjusted -= amount
		reductions = append(reductions, reduction{Index: index, Amount: amount})
		freed += amount
		if freed >= capacity {
			break
		}
	}
	if freed <= 0 {
		return
	}

	leftover := fillLaborProxyToCap(recipients, freed, laborTaxFreeCents, map[string]struct{}{}, rng)
	if leftover > 0 {
		leftover = fillLaborProxyToCap(extraRecipients, leftover, laborProxyHardCapCents, map[string]struct{}{}, rng)
	}
	if leftover > 0 {
		for _, item := range reductions {
			people[item.Index].Adjusted += item.Amount
		}
	}
}

func buildLaborTransferPlan(people []laborPerson, targetTotal int64, originalTotal int64) []laborTransfer {
	sources := make([]laborSource, 0)
	for _, person := range people {
		if person.Original > person.Adjusted {
			sources = append(sources, laborSource{Name: person.Name, Amount: person.Original - person.Adjusted})
		}
	}
	if targetTotal > originalTotal {
		sources = append(sources, laborSource{Name: laborTeamFundSource, Amount: targetTotal - originalTotal})
	}

	receivers := make([]laborReceiver, 0)
	priority := map[string][2]int64{}
	for _, person := range people {
		if person.Adjusted > person.Original {
			receivers = append(receivers, laborReceiver{Name: person.Name, Amount: person.Adjusted - person.Original})
		}
		group := int64(2)
		if person.Original == 0 {
			group = 0
		} else if person.Original < laborTaxFreeCents {
			group = 1
		}
		priority[person.Name] = [2]int64{group, -(person.Adjusted - person.Original)}
	}
	sort.Slice(receivers, func(i, j int) bool {
		left := priority[receivers[i].Name]
		right := priority[receivers[j].Name]
		if left[0] == right[0] {
			return left[1] < right[1]
		}
		return left[0] < right[0]
	})

	transfers := make([]laborTransfer, 0)
	sourceIndex := 0
	receiverIndex := 0
	for sourceIndex < len(sources) && receiverIndex < len(receivers) {
		amount := minInt64(sources[sourceIndex].Amount, receivers[receiverIndex].Amount)
		if amount > 0 {
			transfers = append(transfers, laborTransfer{
				Source:   sources[sourceIndex].Name,
				Receiver: receivers[receiverIndex].Name,
				Amount:   amount,
			})
			sources[sourceIndex].Amount -= amount
			receivers[receiverIndex].Amount -= amount
		}
		if sources[sourceIndex].Amount <= 0 {
			sourceIndex++
		}
		if receivers[receiverIndex].Amount <= 0 {
			receiverIndex++
		}
	}
	return transfers
}

func applyLaborTransferRemarks(people []laborPerson, transfers []laborTransfer) {
	sourceNotes := map[string][]string{}
	receiverNotes := map[string][]string{}
	for _, transfer := range transfers {
		if transfer.Amount <= 0 {
			continue
		}
		amount := formatLaborRemarkAmount(transfer.Amount)
		if transfer.Source == laborTeamFundSource {
			receiverNotes[transfer.Receiver] = append(receiverNotes[transfer.Receiver], fmt.Sprintf("\u56e2\u961f\u7ecf\u8d39\u4ee3\u53d1%s", amount))
			continue
		}
		receiverNotes[transfer.Receiver] = append(receiverNotes[transfer.Receiver], fmt.Sprintf("\u5e2e%s\u4ee3\u53d1%s", transfer.Source, amount))
		sourceNotes[transfer.Source] = append(sourceNotes[transfer.Source], fmt.Sprintf("\u7531%s\u4ee3\u53d1%s", transfer.Receiver, amount))
	}

	for i := range people {
		remarks := make([]string, 0)
		remarks = append(remarks, receiverNotes[people[i].Name]...)
		remarks = append(remarks, sourceNotes[people[i].Name]...)
		people[i].Remarks = remarks
	}
}
func createLaborCalculationWorkbook(result laborAdjustmentResult, rolesByRealName map[string]string, rates RateConfig) ([]byte, error) {
	file := excelize.NewFile()
	defer file.Close()

	sheet := "Sheet1"
	file.SetSheetName("Sheet1", sheet)
	file.SetColWidth(sheet, "A", "A", 7.08203125)
	file.SetColWidth(sheet, "B", "B", 15)
	file.SetColWidth(sheet, "C", "C", 13)
	file.SetColWidth(sheet, "D", "D", 27.58203125)
	file.SetColWidth(sheet, "E", "E", 15.75)
	file.SetColWidth(sheet, "F", "F", 21.83203125)
	file.SetColWidth(sheet, "G", "G", 24.83203125)

	headerStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "\u9ed1\u4f53", Size: 11, Bold: true, Color: "FF0000"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FFFF00"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	ownerNameStyle, _ := file.NewStyle(laborNameStyle("FFC000"))
	leaderNameStyle, _ := file.NewStyle(laborNameStyle("00B0F0"))
	defaultNameStyle, _ := file.NewStyle(laborNameStyle("FFFFFF"))
	bodyStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "\u7b49\u7ebf", Size: 11, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	hoursStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0;[Red]0.0`),
		Font:         &excelize.Font{Family: "\u7b49\u7ebf", Size: 11, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	integerMoneyStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0;[Red]0`),
		Font:         &excelize.Font{Family: "\u7b49\u7ebf", Size: 11, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	totalHoursStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0;[Red]0.0`),
		Font:         &excelize.Font{Family: "\u7b49\u7ebf", Size: 11, Color: "000000"},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{"5B9BD5"}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	totalStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "\u7b49\u7ebf", Size: 11, Color: "000000"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"5B9BD5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	totalIntegerMoneyStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0;[Red]0`),
		Font:         &excelize.Font{Family: "\u7b49\u7ebf", Size: 11, Color: "000000"},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{"5B9BD5"}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	headers := []string{"", "\u503c\u73ed\u5de5\u65f6", "\u5de5\u5355\u5de5\u65f6", "\u5de5\u65f6\u52b3\u52a1\u8d39\u7528\u603b\u8ba1", "\u9879\u76ee\u7ba1\u7406\u8d39\u7528", "\u5e94\u53d1\u52b3\u52a1", "\u5408\u8ba1\u540e\u7684\u5de5\u65f6\u8ba1\u7b97\uff0825\uff09"}
	for col, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		file.SetCellValue(sheet, cell, header)
		if col > 0 {
			file.SetCellStyle(sheet, cell, cell, headerStyle)
		} else {
			file.SetCellStyle(sheet, cell, cell, bodyStyle)
		}
	}

	var totalDutyHours float64
	var totalWorkOrderHours float64
	var totalLaborCost int64
	var totalManagement int64
	var totalPayable int64
	for index, person := range result.People {
		row := index + 2
		dutyHours, workOrderHours, management := laborReportComponents(person, rates)
		dutyCost := int64(math.Round(dutyHours * float64(rates.DutyCents)))
		workOrderCost := int64(math.Round(workOrderHours * float64(rates.WorkOrderCents)))
		laborCost := dutyCost + workOrderCost
		payable := laborCost + management
		managementValue := any(centsToLaborFloat(management))
		if management == 0 {
			managementValue = ""
		}
		totalDutyHours += dutyHours
		totalWorkOrderHours += workOrderHours
		totalLaborCost += laborCost
		totalManagement += management
		totalPayable += payable
		values := []any{
			person.Name,
			roundLaborFloat(dutyHours, 2),
			roundLaborFloat(workOrderHours, 2),
			centsToLaborFloat(laborCost),
			managementValue,
			centsToLaborFloat(payable),
			centsToLaborFloat(payable) / 25,
		}
		for col, value := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			file.SetCellValue(sheet, cell, value)
			style := bodyStyle
			if col == 0 {
				style = laborNameStyleID(rolesByRealName[person.Name], ownerNameStyle, leaderNameStyle, defaultNameStyle)
			}
			if col == 1 || col == 2 || col == 6 {
				style = hoursStyle
			}
			if col == 3 || col == 4 || col == 5 {
				style = integerMoneyStyle
			}
			file.SetCellStyle(sheet, cell, cell, style)
		}
		file.SetRowHeight(sheet, row, 15.5)
	}

	totalRow := len(result.People) + 2
	file.SetCellValue(sheet, fmt.Sprintf("A%d", totalRow), "合计")
	totalValues := []any{
		roundLaborFloat(totalDutyHours, 2),
		roundLaborFloat(totalWorkOrderHours, 2),
		centsToLaborFloat(totalLaborCost),
		laborBlankZeroCents(totalManagement),
		centsToLaborFloat(totalPayable),
		centsToLaborFloat(totalPayable) / 25,
	}
	file.SetCellStyle(sheet, fmt.Sprintf("A%d", totalRow), fmt.Sprintf("A%d", totalRow), totalStyle)
	for index, value := range totalValues {
		cell, _ := excelize.CoordinatesToCellName(index+2, totalRow)
		file.SetCellValue(sheet, cell, value)
		style := totalIntegerMoneyStyle
		if index == 0 || index == 1 || index == 5 {
			style = totalHoursStyle
		}
		file.SetCellStyle(sheet, cell, cell, style)
	}

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func createLaborWorkStudyConversionWorkbook(people []laborPerson, outputMonth string) ([]byte, error) {
	file := excelize.NewFile()
	defer file.Close()

	sheet := "Sheet1"
	file.SetSheetName("Sheet1", sheet)
	file.SetColWidth(sheet, "A", "A", 25.75)
	file.SetColWidth(sheet, "B", "B", 27.5)
	file.SetColWidth(sheet, "C", "C", 29.75)
	file.SetColWidth(sheet, "D", "D", 29)
	file.SetColWidth(sheet, "E", "E", 11.75)

	titleStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "\u5b8b\u4f53", Size: 22, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    laborCellBorders("000000"),
	})
	tableStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "\u5b8b\u4f53", Size: 18, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    laborCellBorders("000000"),
	})
	hourStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0;[Red]0.0`),
		Font:         &excelize.Font{Family: "\u5b8b\u4f53", Size: 18, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:       laborCellBorders("000000"),
	})
	amountStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0_ `),
		Font:         &excelize.Font{Family: "\u5b8b\u4f53", Size: 18, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:       laborCellBorders("000000"),
	})

	file.MergeCell(sheet, "A1", "D1")
	file.SetCellValue(sheet, "A1", laborWorkStudyTitle(outputMonth))
	file.SetCellStyle(sheet, "A1", "D1", titleStyle)
	file.SetRowHeight(sheet, 1, 27.5)

	headers := []string{"\u59d3\u540d", "\u5de5\u4f5c\u65f6\u957f\uff08h\uff09", "\u6d4b\u7b97\u6807\u51c6", "\u5e94\u53d1\u52b3\u52a1\u8d39\uff08\u5143\uff09"}
	for index, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(index+1, 2)
		file.SetCellValue(sheet, cell, header)
		file.SetCellStyle(sheet, cell, cell, tableStyle)
	}
	file.SetRowHeight(sheet, 2, 23)

	for index, person := range people {
		row := index + 3
		file.SetCellValue(sheet, fmt.Sprintf("A%d", row), person.Name)
		file.SetCellValue(sheet, fmt.Sprintf("B%d", row), centsToLaborFloat(person.Adjusted)/25)
		file.SetCellValue(sheet, fmt.Sprintf("C%d", row), "25\u5143/\u5c0f\u65f6")
		file.SetCellFormula(sheet, fmt.Sprintf("D%d", row), fmt.Sprintf("B%d*25", row))
		file.SetCellStyle(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), tableStyle)
		file.SetCellStyle(sheet, fmt.Sprintf("B%d", row), fmt.Sprintf("B%d", row), hourStyle)
		file.SetCellStyle(sheet, fmt.Sprintf("C%d", row), fmt.Sprintf("C%d", row), tableStyle)
		file.SetCellStyle(sheet, fmt.Sprintf("D%d", row), fmt.Sprintf("D%d", row), amountStyle)
		file.SetRowHeight(sheet, row, 23)
	}

	totalRow := len(people) + 3
	file.SetCellValue(sheet, fmt.Sprintf("A%d", totalRow), "\u603b\u8ba1")
	if len(people) > 0 {
		file.SetCellFormula(sheet, fmt.Sprintf("B%d", totalRow), fmt.Sprintf("SUM(B3:B%d)", totalRow-1))
		file.SetCellFormula(sheet, fmt.Sprintf("D%d", totalRow), fmt.Sprintf(`"总计："&SUM(D3:D%d)&" 元"`, totalRow-1))
	} else {
		file.SetCellValue(sheet, fmt.Sprintf("B%d", totalRow), 0)
		file.SetCellValue(sheet, fmt.Sprintf("D%d", totalRow), "\u603b\u8ba1\uff1a0 \u5143")
	}
	file.SetCellValue(sheet, fmt.Sprintf("C%d", totalRow), "25\u5143/\u5c0f\u65f6")
	file.SetCellStyle(sheet, fmt.Sprintf("A%d", totalRow), fmt.Sprintf("A%d", totalRow), hourStyle)
	file.SetCellStyle(sheet, fmt.Sprintf("B%d", totalRow), fmt.Sprintf("B%d", totalRow), hourStyle)
	file.SetCellStyle(sheet, fmt.Sprintf("C%d", totalRow), fmt.Sprintf("C%d", totalRow), tableStyle)
	file.SetCellStyle(sheet, fmt.Sprintf("D%d", totalRow), fmt.Sprintf("D%d", totalRow), tableStyle)
	file.SetRowHeight(sheet, totalRow, 23)

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func laborWorkStudyTitle(outputMonth string) string {
	month, err := parseLaborCSVOutputMonth(outputMonth)
	if err != nil {
		month = time.Now()
	}
	return fmt.Sprintf("%d\u5e74%d\u6708\u4efd\u673a\u623f\u8fd0\u8425\u9879\u76ee\u52b3\u52a1\u8d39(30\u95f4\u673a\u623f)", month.Year(), int(month.Month()))
}

func laborMonthFilenamePrefix(outputMonth string) string {
	month, err := parseLaborCSVOutputMonth(outputMonth)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d\u5e74%02d\u6708", month.Year(), int(month.Month()))
}

func laborBlankZeroCents(amount int64) any {
	if amount == 0 {
		return ""
	}
	return centsToLaborFloat(amount)
}

func laborNameStyle(fillColor string) *excelize.Style {
	return &excelize.Style{
		Font:      &excelize.Font{Family: "\u7b49\u7ebf", Size: 12},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{fillColor}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    laborCellBorders("000000"),
	}
}

func laborNameStyleID(role string, ownerStyle int, leaderStyle int, defaultStyle int) int {
	switch strings.TrimSpace(role) {
	case "OWNER":
		return ownerStyle
	case "LEADER", "HR":
		return leaderStyle
	default:
		return defaultStyle
	}
}

func createLaborAdjustedCSV(result laborAdjustmentResult, outputMonth string) ([]byte, error) {
	outputMonthStart, err := parseLaborCSVOutputMonth(outputMonth)
	if err != nil {
		return nil, err
	}

	allocator := newCSVScheduleAllocator()
	entries := make([]dutyCSVEntry, 0, len(result.People)*8)
	dateOrder := dateOrderFrom(outputMonthStart, outputMonthStart)
	for _, person := range result.People {
		if person.Adjusted <= 0 {
			continue
		}
		if person.Adjusted%laborStepCents != 0 {
			return nil, fmt.Errorf("%s 调整后金额必须是 25 元的整数倍", person.Name)
		}
		allocated, err := allocator.allocate(person.Name, outputMonthStart, int(person.Adjusted/laborStepCents)*60, dateOrder, csvNormalWorkBlocks(), csvExtendedWorkBlocks())
		if err != nil {
			return nil, err
		}
		entries = append(entries, allocated...)
	}
	sortDutyCSVEntries(entries)
	return writeDutyCSVEntries(entries)
}

func (s *Store) createLaborAdjustedCSV(result laborAdjustmentResult, options laborRunOptions) ([]byte, error) {
	if strings.TrimSpace(options.SourceFinanceBatchID) == "" {
		return createLaborAdjustedCSV(result, options.CSVOutputMonth)
	}

	batch, err := s.readFinanceLocalBatch(options.SourceFinanceBatchID)
	if err != nil {
		return nil, err
	}
	start, end, err := parseAllowedDateRange(batch.StartDate, batch.EndDate)
	if err != nil {
		return nil, err
	}
	outputMonthStart, err := parseCSVOutputMonth(batch.OutputMonth, start)
	if err != nil {
		return nil, err
	}
	dutyEntries, err := s.getDutyCSVEntriesInDateRange(start, end)
	if err != nil {
		return nil, err
	}
	workOrders, err := s.ListWorkOrdersByIDs(batch.WorkOrderIDs)
	if err != nil {
		return nil, err
	}
	workOrders = filterWorkOrdersByDateRangeExportMonths(workOrders, start, end)

	managementPeople := []csvManagementPerson{}
	if batch.IncludeManagement && batch.ManagementMonths > 0 {
		users, err := s.financeSummaryUsers()
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if s.calculateManagementAmountForMonthCount(user.Role, batch.ManagementMonths) <= 0 {
				continue
			}
			managementPeople = append(managementPeople, csvManagementPerson{Name: user.RealName, Role: user.Role})
		}
	}

	entries, err := buildLaborAdjustedCSVEntriesWithPriority(outputMonthStart, result, dutyEntries, workOrders, managementPeople, batch.ManagementMonths, s.rates)
	if err != nil {
		return nil, err
	}
	return writeDutyCSVEntries(entries)
}

func buildLaborAdjustedCSVEntriesWithPriority(outputMonthStart time.Time, result laborAdjustmentResult, dutyEntries []dutyCSVEntry, workOrders []types.WorkOrder, managementPeople []csvManagementPerson, managementMonths int, rates RateConfig) ([]dutyCSVEntry, error) {
	allocator := newCSVScheduleAllocator()
	entries := []dutyCSVEntry{}
	remaining := map[string]int{}
	for _, person := range result.People {
		if person.Adjusted <= 0 {
			continue
		}
		if person.Adjusted%laborStepCents != 0 {
			return nil, fmt.Errorf("%s \u8c03\u6574\u540e\u91d1\u989d\u5fc5\u987b\u662f 25 \u5143\u7684\u6574\u6570\u500d", person.Name)
		}
		remaining[person.Name] = int(person.Adjusted/laborStepCents) * 60
	}

	appendAllocated := func(name string, allocated []dutyCSVEntry, minutes int) {
		if minutes <= 0 {
			return
		}
		entries = append(entries, allocated...)
		remaining[name] -= minutes
	}

	for _, entry := range dutyEntries {
		minutes := minInt(hoursToMinutes(entry.Hours), remaining[entry.Name])
		if minutes <= 0 {
			continue
		}
		allocated, err := allocatePreferredDutyCSVEntry(allocator, outputMonthStart, entry, minutes)
		if err != nil {
			return nil, err
		}
		appendAllocated(entry.Name, allocated, minutes)
	}

	for _, workOrder := range workOrders {
		for _, session := range workOrder.WorkSessions {
			name := strings.TrimSpace(session.WorkerName)
			minutes := minInt(hoursToMinutes(session.Duration*2), remaining[name])
			if name == "" || minutes <= 0 {
				continue
			}
			sessionDate, err := time.Parse("2006-01-02", strings.TrimSpace(session.Date))
			if err != nil {
				return nil, ErrInvalidDateRange
			}
			mappedDate := mapDateToOutputMonth(sessionDate, outputMonthStart)
			allocated, err := allocator.allocate(name, mappedDate, minutes, dateOrderFrom(outputMonthStart, mappedDate), csvNormalWorkBlocks(), csvExtendedWorkBlocks())
			if err != nil {
				return nil, err
			}
			appendAllocated(name, allocated, minutes)
		}
	}

	if managementMonths > 0 {
		for _, person := range managementPeople {
			amount := float64(managementMonths) * float64(rates.mgmtCentsForRole(person.Role)) / 100
			if amount <= 0 {
				continue
			}
			minutes := minInt(hoursToMinutes(amount/rates.DutyYuan()), remaining[person.Name])
			if minutes <= 0 {
				continue
			}
			allocated, err := allocator.allocate(person.Name, firstSaturday(outputMonthStart), minutes, managementDateOrder(outputMonthStart), csvNormalWorkBlocks(), csvExtendedWorkBlocks())
			if err != nil {
				return nil, err
			}
			appendAllocated(person.Name, allocated, minutes)
		}
	}

	for _, person := range result.People {
		minutes := remaining[person.Name]
		if minutes <= 0 {
			continue
		}
		allocated, err := allocator.allocate(person.Name, firstSaturday(outputMonthStart), minutes, managementDateOrder(outputMonthStart), csvNormalWorkBlocks(), csvExtendedWorkBlocks())
		if err != nil {
			return nil, err
		}
		appendAllocated(person.Name, allocated, minutes)
	}

	sortDutyCSVEntries(entries)
	return entries, nil
}

func allocatePreferredDutyCSVEntry(allocator *csvScheduleAllocator, outputMonthStart time.Time, entry dutyCSVEntry, minutes int) ([]dutyCSVEntry, error) {
	mappedDate := mapDateToOutputMonth(entry.Date, outputMonthStart)
	startMinute, endMinute, ok := parseCSVTimeRange(entry.StartTime, entry.EndTime)
	if ok {
		block := csvTimeBlock{Start: startMinute, End: startMinute + minutes}
		if block.End <= endMinute && !allocator.hasOverlap(entry.Name, mappedDate, block) {
			allocator.occupy(entry.Name, mappedDate, block)
			return []dutyCSVEntry{{
				Name:      entry.Name,
				Date:      mappedDate,
				StartTime: formatCSVMinute(block.Start),
				EndTime:   formatCSVMinute(block.End),
				Hours:     float64(minutes) / 60,
			}}, nil
		}
	}
	return allocator.allocate(entry.Name, mappedDate, minutes, dateOrderFrom(outputMonthStart, mappedDate), csvNormalWorkBlocks(), csvExtendedWorkBlocks())
}

func parseLaborCSVOutputMonth(outputMonth string) (time.Time, error) {
	month := strings.TrimSpace(outputMonth)
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	outputMonthStart, err := time.Parse("2006-01", month)
	if err != nil {
		return time.Time{}, ErrInvalidDateRange
	}
	return time.Date(outputMonthStart.Year(), outputMonthStart.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

func normalizeLaborCSVOutputMonth(outputMonth string, inputFilename string) string {
	if _, err := parseLaborCSVOutputMonth(outputMonth); err == nil && strings.TrimSpace(outputMonth) != "" {
		return strings.TrimSpace(outputMonth)
	}
	if inferred := inferLaborMonthFromFilename(inputFilename); inferred != "" {
		return inferred
	}
	return time.Now().Format("2006-01")
}

func inferLaborMonthFromFilename(filename string) string {
	matches := regexp.MustCompile(`20\d{2}[-年]?\d{2}`).FindAllString(filename, -1)
	if len(matches) == 0 {
		return ""
	}
	value := matches[len(matches)-1]
	value = strings.ReplaceAll(value, "年", "")
	value = strings.ReplaceAll(value, "-", "")
	if len(value) != 6 {
		return ""
	}
	month := value[:4] + "-" + value[4:]
	if _, err := time.Parse("2006-01", month); err != nil {
		return ""
	}
	return month
}

func buildLaborResponse(id, createdAt string, seed *int64, result laborAdjustmentResult, options laborRunOptions) types.LaborConvertResponse {
	rows := make([]types.LaborConvertRow, 0, len(result.People))
	for _, person := range result.People {
		tax := estimateLaborTax(person.Adjusted)
		rows = append(rows, types.LaborConvertRow{
			Name:     person.Name,
			Original: formatLaborMoney(person.Original),
			Adjusted: formatLaborMoney(person.Adjusted),
			Delta:    formatLaborMoney(person.Adjusted - person.Original),
			Tax:      formatLaborMoney(tax),
			Net:      formatLaborMoney(person.Adjusted - tax),
			Remark:   strings.Join(person.Remarks, "; "),
		})
	}

	noiseItems := make([]types.LaborConvertNoiseItem, 0, len(result.Noise.Items))
	for _, item := range result.Noise.Items {
		noiseItems = append(noiseItems, types.LaborConvertNoiseItem{Name: item.Name, Reduction: formatLaborMoney(item.Reduction)})
	}

	transfers := make([]types.LaborConvertTransfer, 0, len(result.Transfers))
	for _, transfer := range result.Transfers {
		transfers = append(transfers, types.LaborConvertTransfer{
			Source:   transfer.Source,
			Receiver: transfer.Receiver,
			Amount:   formatLaborMoney(transfer.Amount),
		})
	}

	return types.LaborConvertResponse{
		HistoryID:            id,
		CreatedAt:            createdAt,
		InputFilename:        options.InputFilename,
		OutputName:           options.OutputName,
		DownloadURL:          fmt.Sprintf("/api/labor-convert/history/%s/download", id),
		CSVName:              options.CSVName,
		CSVDownloadURL:       fmt.Sprintf("/api/labor-convert/history/%s/download/csv", id),
		HasCSV:               options.CSVName != "",
		CSVOutputMonth:       options.CSVOutputMonth,
		SourceFinanceBatchID: options.SourceFinanceBatchID,
		ParentRunID:          options.ParentRunID,
		IsManualAdjust:       options.IsManualAdjust,
		CanManualAdjust:      true,
		Seed:                 seed,
		Summary: types.LaborConvertSummary{
			OriginalTotal: formatLaborMoney(result.OriginalTotal),
			TargetTotal:   formatLaborMoney(result.TargetTotal),
			FinalTotal:    formatLaborMoney(result.FinalTotal),
			TeamFund:      formatLaborMoney(result.TeamFund),
			Warnings:      result.Warnings,
			Noise:         types.LaborConvertNoise{Applied: result.Noise.Applied, Items: noiseItems},
		},
		Rows:      rows,
		Transfers: transfers,
	}
}

func laborReportComponents(person laborPerson, rates RateConfig) (float64, float64, int64) {
	dutyHours := person.DutyHours
	dutyCost := int64(math.Round(dutyHours * float64(rates.DutyCents)))
	if dutyCost > person.Adjusted {
		return float64(person.Adjusted) / float64(rates.DutyCents), 0, 0
	}
	remaining := person.Adjusted - dutyCost
	management := minInt64(person.Management, remaining)
	remaining -= management
	workOrderHours := float64(remaining) / float64(rates.WorkOrderCents)
	return dutyHours, workOrderHours, management
}

func laborOptionalMoney(value int64) any {
	if value == 0 {
		return nil
	}
	return float64(value) / 100
}

func centsToLaborFloat(value int64) float64 {
	return float64(value) / 100
}

func stringPointer(value string) *string {
	return &value
}

func normalizeLaborHeader(value string) string {
	return strings.Join(strings.Fields(value), "")
}

func findLaborHeader(labels []string, keywords []string) int {
	for _, keyword := range keywords {
		for index, label := range labels {
			if strings.Contains(label, keyword) {
				return index
			}
		}
	}
	return -1
}

func findLaborAmountHeader(labels []string) int {
	exact := []string{"\u603b\u52b3\u52a1", "\u603b\u91d1\u989d", "\u5e94\u53d1\u52b3\u52a1", "\u5e94\u53d1\u91d1\u989d", "\u5e94\u53d1", "\u5408\u8ba1\u91d1\u989d", "amount", "total"}
	for _, target := range exact {
		for index, label := range labels {
			if label == target {
				return index
			}
		}
	}
	for index, label := range labels {
		if strings.Contains(label, "\u603b") && (strings.Contains(label, "\u916c\u52b3") || strings.Contains(label, "\u91d1\u989d") || strings.Contains(label, "\u52b3\u52a1") || strings.Contains(label, "\u5de5\u8d44")) {
			return index
		}
	}
	for index, label := range labels {
		if strings.Contains(label, "\u5e94\u53d1") || strings.Contains(label, "\u5b9e\u53d1") {
			return index
		}
	}
	return -1
}

func isLaborSummaryRow(name string) bool {
	switch strings.TrimSpace(name) {
	case "\u5408\u8ba1", "\u603b\u8ba1", "\u7edf\u8ba1\u8303\u56f4", "\u5de5\u5355\u6570", "\u5305\u542b\u5de5\u5355":
		return true
	default:
		return false
	}
}

func laborCellToCents(value string) (int64, error) {
	text := strings.TrimSpace(value)
	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimPrefix(text, "¥")
	text = strings.TrimPrefix(text, "￥")
	text = strings.TrimPrefix(text, "楼")
	if text == "" {
		return 0, fmt.Errorf("empty")
	}
	amount, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, err
	}
	return int64(math.Round(amount * 100)), nil
}

func laborCellToCentsOrZero(row []string, column int) int64 {
	if column < 0 || column >= len(row) {
		return 0
	}
	value, err := laborCellToCents(row[column])
	if err != nil {
		return 0
	}
	return value
}

func laborCellToFloat(row []string, column int) float64 {
	if column < 0 || column >= len(row) {
		return 0
	}
	text := strings.TrimSpace(strings.ReplaceAll(row[column], ",", ""))
	if text == "" {
		return 0
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}
	return value
}

func estimateLaborTax(amount int64) int64 {
	if amount <= laborTaxFreeCents {
		return 0
	}
	return ((amount - laborTaxFreeCents) * 20) / 100
}

func formatLaborMoney(value int64) string {
	return fmt.Sprintf("%.2f", float64(value)/100)
}

func formatLaborRemarkAmount(value int64) string {
	yuan := value / 100
	if value%100 == 0 {
		return fmt.Sprintf("%d\u5143", yuan)
	}
	return fmt.Sprintf("%.2f\u5143", float64(value)/100)
}

func safeLaborStem(filename string) string {
	stem := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	if strings.TrimSpace(stem) == "" {
		stem = "DMS财务统计"
	}
	replacer := strings.NewReplacer("<", "-", ">", "-", ":", "-", `"`, "-", "/", "-", `\`, "-", "|", "-", "?", "-", "*", "-")
	return replacer.Replace(stem)
}

func newLaborRunID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := crand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:]), nil
}

func laborPeopleRefs(people []laborPerson, predicate func(laborPerson) bool) []*laborPerson {
	result := make([]*laborPerson, 0)
	for i := range people {
		if predicate == nil || predicate(people[i]) {
			result = append(result, &people[i])
		}
	}
	return result
}

func laborCapacity(people []*laborPerson, cap int64, step int64) int64 {
	var total int64
	if step <= 0 {
		step = laborStepCents
	}
	for _, person := range people {
		if person.Adjusted < cap {
			total += ((cap - person.Adjusted) / step) * step
		}
	}
	return total
}

func sumLaborOriginal(people []laborPerson) int64 {
	var total int64
	for _, person := range people {
		total += person.Original
	}
	return total
}

func sumLaborAdjusted(people []laborPerson) int64 {
	var total int64
	for _, person := range people {
		total += person.Adjusted
	}
	return total
}

func laborCellBorders(color string) []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: color, Style: 1},
		{Type: "right", Color: color, Style: 1},
		{Type: "top", Color: color, Style: 1},
		{Type: "bottom", Color: color, Style: 1},
	}
}

func roundLaborFloat(value float64, precision int) float64 {
	scale := math.Pow10(precision)
	return math.Round(value*scale) / scale
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
