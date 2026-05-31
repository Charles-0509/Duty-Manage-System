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

func ParseLaborMoneyToCents(value string) (int64, error) {
	text := strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	text = strings.TrimPrefix(text, "楼")
	if text == "" {
		return 0, fmt.Errorf("money cannot be empty")
	}
	amount, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid money format: %s", value)
	}
	return int64(math.Round(amount * 100)), nil
}

func (s *Store) ConvertLaborWorkbook(content []byte, inputFilename string, targetTotal int64, seed *int64) (types.LaborConvertResponse, error) {
	if targetTotal <= 0 {
		return types.LaborConvertResponse{}, fmt.Errorf("閻╊喗鐖ｉ幀鑽ょ病鐠愮懓绻€妞よ銇囨禍?0")
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

	workbook, err := createLaborCalculationWorkbook(result, rolesByRealName)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}

	id, err := newLaborRunID()
	if err != nil {
		return types.LaborConvertResponse{}, err
	}
	createdAt := time.Now().Format("2006-01-02 15:04:05")
	outputName := fmt.Sprintf("%s-閸斿啿濮熺拋锛勭暬.xlsx", safeLaborStem(inputFilename))
	response := buildLaborResponse(id, createdAt, inputFilename, outputName, effectiveSeed, result)

	if err := s.saveLaborConversionRun(response, result, workbook); err != nil {
		return types.LaborConvertResponse{}, err
	}
	return response, nil
}

func (s *Store) ListLaborConversionRuns() ([]types.LaborConvertHistoryItem, error) {
	rows, err := s.db.Query(`
		SELECT id, created_at, input_filename, output_name, target_total_cents, final_total_cents
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
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.InputFilename, &item.OutputName, &targetTotal, &finalTotal); err != nil {
			return nil, err
		}
		item.TargetTotal = formatLaborMoney(targetTotal)
		item.FinalTotal = formatLaborMoney(finalTotal)
		item.DownloadURL = fmt.Sprintf("/api/labor-convert/history/%s/download", item.ID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetLaborConversionRun(id string) (types.LaborConvertResponse, error) {
	if !laborHistoryIDPattern.MatchString(id) {
		return types.LaborConvertResponse{}, sql.ErrNoRows
	}
	var payload string
	err := s.db.QueryRow(`SELECT result_json FROM labor_conversion_runs WHERE id = ?`, id).Scan(&payload)
	if err != nil {
		return types.LaborConvertResponse{}, err
	}

	var response types.LaborConvertResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return types.LaborConvertResponse{}, err
	}
	return response, nil
}

func (s *Store) GetLaborConversionWorkbook(id string) (string, []byte, error) {
	if !laborHistoryIDPattern.MatchString(id) {
		return "", nil, sql.ErrNoRows
	}
	var filename string
	var content []byte
	err := s.db.QueryRow(`
		SELECT output_name, workbook_blob
		FROM labor_conversion_runs
		WHERE id = ?
	`, id).Scan(&filename, &content)
	if err != nil {
		return "", nil, err
	}
	return filename, content, nil
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

func (s *Store) saveLaborConversionRun(response types.LaborConvertResponse, result laborAdjustmentResult, workbook []byte) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}

	var seed any
	if response.Seed != nil {
		seed = *response.Seed
	}

	_, err = s.db.Exec(`
		INSERT INTO labor_conversion_runs
			(id, created_at, input_filename, output_name, target_total_cents, original_total_cents, final_total_cents, team_fund_cents, seed, result_json, workbook_blob)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, response.HistoryID, response.CreatedAt, response.InputFilename, response.OutputName, result.TargetTotal, result.OriginalTotal, result.FinalTotal, result.TeamFund, seed, string(payload), workbook)
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
		return laborAdjustmentResult{}, fmt.Errorf("閻╊喗鐖ｉ幀鑽ょ病鐠?%s 鐡掑懎鍤ぐ鎾冲娴滃搫鎲抽崣顖涘閹恒儰绗傞梽?%s閿涘矁顕梽宥勭秵閻╊喗鐖ｉ幀濠氼杺閹存牕顤冮崝鐘插讲娴狅絽褰傛禍鍝勬喅", formatLaborMoney(targetTotal), formatLaborMoney(maxTotal))
	}

	warnings := []string{}
	if targetTotal >= baseTotal {
		remaining := targetTotal - baseTotal
		remaining = allocateLaborSurplus(adjustedPeople, remaining, map[string]struct{}{}, rng)
		if remaining > 0 {
			warnings = append(warnings, "priority quota was insufficient; filled up to the 2000 monthly cap")
			remaining = randomLaborFillToCap(laborPeopleRefs(adjustedPeople, nil), remaining, laborMaxPersonCents, map[string]struct{}{}, rng)
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
				leftover = randomLaborFillToCap(laborPeopleRefs(adjustedPeople, nil), leftover, laborMaxPersonCents, map[string]struct{}{}, rng)
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

	amount = randomLaborFillToCap(zeroOriginals, amount, laborTaxFreeCents, excluded, rng)
	amount = randomLaborFillToCap(lowOriginals, amount, laborTaxFreeCents, excluded, rng)
	amount = randomLaborFillToCap(laborPeopleRefs(people, nil), amount, laborProxyHardCapCents, excluded, rng)
	return amount
}

func randomLaborFillToCap(people []*laborPerson, amount int64, cap int64, excluded map[string]struct{}, rng *mrand.Rand) int64 {
	if amount <= 0 {
		return amount
	}
	eligible := make([]int, 0)
	var totalCapacity int64
	for i := range people {
		if _, ok := excluded[people[i].Name]; ok || people[i].Adjusted >= cap {
			continue
		}
		eligible = append(eligible, i)
		totalCapacity += ((cap - people[i].Adjusted) / laborStepCents) * laborStepCents
	}
	if len(eligible) == 0 || totalCapacity <= 0 {
		return amount
	}
	if amount >= totalCapacity {
		for _, index := range eligible {
			people[index].Adjusted += ((cap - people[index].Adjusted) / laborStepCents) * laborStepCents
		}
		return amount - totalCapacity
	}

	stepsLeft := amount / laborStepCents
	for stepsLeft > 0 {
		candidates := make([]int, 0)
		for _, index := range eligible {
			if people[index].Adjusted+laborStepCents <= cap {
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
		capacitySteps := (cap - people[pick].Adjusted) / laborStepCents
		chunkSteps := int64(rng.Intn(int(minInt64(capacitySteps, minInt64(stepsLeft, 4)))) + 1)
		people[pick].Adjusted += chunkSteps * laborStepCents
		stepsLeft -= chunkSteps
	}
	return stepsLeft * laborStepCents
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

	choices := []int64{laborNoiseMinCents, laborNoiseMinCents + laborStepCents, laborNoiseMaxCents}
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
		leftover = randomLaborFillToCap(laborPeopleRefs(people, nil), leftover, laborMaxPersonCents, selectedSet, rng)
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
	capacity := laborCapacity(recipients, laborTaxFreeCents)
	if capacity < laborStepCents {
		capacity = laborCapacity(extraRecipients, laborProxyHardCapCents)
	}
	if capacity < laborStepCents {
		return
	}

	rng.Shuffle(len(zeroHelpers), func(i, j int) { zeroHelpers[i], zeroHelpers[j] = zeroHelpers[j], zeroHelpers[i] })
	selectedCount := maxInt(1, len(zeroHelpers)-1)
	choices := []int64{laborStepCents, laborStepCents * 2, laborStepCents * 3, laborStepCents * 4}
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

	leftover := randomLaborFillToCap(recipients, freed, laborTaxFreeCents, map[string]struct{}{}, rng)
	if leftover > 0 {
		leftover = randomLaborFillToCap(extraRecipients, leftover, laborProxyHardCapCents, map[string]struct{}{}, rng)
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
func createLaborCalculationWorkbook(result laborAdjustmentResult, rolesByRealName map[string]string) ([]byte, error) {
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
		Font:      &excelize.Font{Family: "Arial", Size: 11, Bold: true, Color: "FF0000"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FFFF00"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	ownerNameStyle, _ := file.NewStyle(laborNameStyle("FFC000"))
	leaderNameStyle, _ := file.NewStyle(laborNameStyle("00B0F0"))
	defaultNameStyle, _ := file.NewStyle(laborNameStyle("FFFFFF"))
	bodyStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Arial", Size: 11, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	moneyStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.00_);[Red]\(0.00\)`),
		Font:         &excelize.Font{Family: "Arial", Size: 11, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	oneDecimalStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0;[Red]0.0`),
		Font:         &excelize.Font{Family: "Arial", Size: 11, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	payableStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0_ `),
		Font:         &excelize.Font{Family: "Arial", Size: 11, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	totalStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Arial", Size: 11, Color: "000000"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"5B9BD5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	totalMoneyStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.00_);[Red]\(0.00\)`),
		Font:         &excelize.Font{Family: "Arial", Size: 11, Color: "000000"},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{"5B9BD5"}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	headers := []string{"", "Duty hours", "Work order hours", "Labor fee total", "Management fee", "Payable labor", "Adjusted payable"}
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
		dutyHours, workOrderHours, management := laborReportComponents(person)
		dutyCost := int64(math.Round(dutyHours * 2500))
		workOrderCost := int64(math.Round(workOrderHours * 5000))
		laborCost := dutyCost + workOrderCost
		payable := laborCost + management
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
			centsToLaborFloat(management),
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
			if col == 1 || col == 2 || col == 3 || col == 6 {
				style = moneyStyle
			}
			if col == 4 {
				style = oneDecimalStyle
			}
			if col == 5 {
				style = payableStyle
			}
			file.SetCellStyle(sheet, cell, cell, style)
		}
		file.SetRowHeight(sheet, row, 15.5)
	}

	totalRow := len(result.People) + 2
	file.SetCellValue(sheet, fmt.Sprintf("A%d", totalRow), "Total")
	totalValues := []any{
		roundLaborFloat(totalDutyHours, 2),
		roundLaborFloat(totalWorkOrderHours, 2),
		centsToLaborFloat(totalLaborCost),
		centsToLaborFloat(totalManagement),
		centsToLaborFloat(totalPayable),
		centsToLaborFloat(totalPayable) / 25,
	}
	file.SetCellStyle(sheet, fmt.Sprintf("A%d", totalRow), fmt.Sprintf("A%d", totalRow), totalStyle)
	for index, value := range totalValues {
		cell, _ := excelize.CoordinatesToCellName(index+2, totalRow)
		file.SetCellValue(sheet, cell, value)
		file.SetCellStyle(sheet, cell, cell, totalMoneyStyle)
	}

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func laborNameStyle(fillColor string) *excelize.Style {
	return &excelize.Style{
		Font:      &excelize.Font{Family: "Arial", Size: 12},
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

func buildLaborResponse(id, createdAt, inputFilename, outputName string, seed *int64, result laborAdjustmentResult) types.LaborConvertResponse {
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
		HistoryID:     id,
		CreatedAt:     createdAt,
		InputFilename: inputFilename,
		OutputName:    outputName,
		DownloadURL:   fmt.Sprintf("/api/labor-convert/history/%s/download", id),
		Seed:          seed,
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

func laborReportComponents(person laborPerson) (float64, float64, int64) {
	dutyHours := person.DutyHours
	dutyCost := int64(math.Round(dutyHours * 2500))
	if dutyCost > person.Adjusted {
		return float64(person.Adjusted) / 100 / 25, 0, 0
	}
	remaining := person.Adjusted - dutyCost
	management := minInt64(person.Management, remaining)
	remaining -= management
	workOrderHours := float64(remaining) / 100 / 50
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
		stem = "DMS鐠愩垹濮熺紒鐔活吀"
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

func laborCapacity(people []*laborPerson, cap int64) int64 {
	var total int64
	for _, person := range people {
		if person.Adjusted < cap {
			total += ((cap - person.Adjusted) / laborStepCents) * laborStepCents
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
