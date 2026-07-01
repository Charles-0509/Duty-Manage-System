package store

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestAdjustLaborKeepsTwentyFiveYuanSteps(t *testing.T) {
	seed := int64(7)
	result, err := adjustLabor([]laborPerson{
		{Name: "A", Original: 160000},
		{Name: "B", Original: 0},
		{Name: "C", Original: 0},
		{Name: "D", Original: 50000},
	}, 320000, &seed)
	if err != nil {
		t.Fatalf("adjustLabor returned error: %v", err)
	}

	for _, person := range result.People {
		if person.Adjusted%laborStepCents != 0 {
			t.Fatalf("%s adjusted amount %d is not a 25-yuan step", person.Name, person.Adjusted)
		}
	}
	if result.FinalTotal != 320000 {
		t.Fatalf("final total = %d, want 320000", result.FinalTotal)
	}
}

func TestProxyAndTeamFundUseFiftyYuanSteps(t *testing.T) {
	seed := int64(23)
	result, err := adjustLabor([]laborPerson{
		{Name: "A", Original: 80000},
		{Name: "B", Original: 0},
		{Name: "C", Original: 0},
		{Name: "D", Original: 50000},
	}, 280000, &seed)
	if err != nil {
		t.Fatalf("adjustLabor returned error: %v", err)
	}

	if result.TeamFund%laborProxyStepCents != 0 {
		t.Fatalf("team fund = %d, want multiple of %d", result.TeamFund, laborProxyStepCents)
	}
	for _, person := range result.People {
		if person.Adjusted > person.Original {
			delta := person.Adjusted - person.Original
			if delta%laborProxyStepCents != 0 {
				t.Fatalf("%s proxy delta = %d, want multiple of %d", person.Name, delta, laborProxyStepCents)
			}
		}
	}
	for _, transfer := range result.Transfers {
		if transfer.Source == laborTeamFundSource || transfer.Receiver != "" {
			if transfer.Amount%laborProxyStepCents != 0 {
				t.Fatalf("transfer %#v amount is not a 50-yuan step", transfer)
			}
		}
	}
}

func TestProxyAllowsSingleTwentyFiveYuanTailWhenNeeded(t *testing.T) {
	seed := int64(29)
	result, err := adjustLabor([]laborPerson{
		{Name: "A", Original: 80000},
		{Name: "B", Original: 0},
	}, 82500, &seed)
	if err != nil {
		t.Fatalf("adjustLabor returned error: %v", err)
	}

	if result.TeamFund != laborStepCents {
		t.Fatalf("team fund = %d, want %d", result.TeamFund, laborStepCents)
	}
	oddProxyPeople := 0
	for _, person := range result.People {
		if person.Adjusted > person.Original && (person.Adjusted-person.Original)%laborProxyStepCents == laborStepCents {
			oddProxyPeople++
		}
	}
	if oddProxyPeople != 1 {
		t.Fatalf("odd proxy people = %d, want 1; people=%#v", oddProxyPeople, result.People)
	}
	oddTransfers := 0
	for _, transfer := range result.Transfers {
		if transfer.Amount%laborProxyStepCents == laborStepCents {
			oddTransfers++
		}
	}
	if oddTransfers != 1 {
		t.Fatalf("odd transfers = %d, want 1; transfers=%#v", oddTransfers, result.Transfers)
	}
}

func TestZeroOriginalHelpersGetVariationWhenPossible(t *testing.T) {
	seed := int64(11)
	result, err := adjustLabor([]laborPerson{
		{Name: "A", Original: 80000},
		{Name: "B", Original: 0},
		{Name: "C", Original: 0},
		{Name: "D", Original: 50000},
	}, 290000, &seed)
	if err != nil {
		t.Fatalf("adjustLabor returned error: %v", err)
	}

	amounts := map[int64]bool{}
	zeroHelpers := 0
	for _, person := range result.People {
		if person.Original == 0 && person.Adjusted > 0 {
			zeroHelpers++
			amounts[person.Adjusted] = true
		}
	}
	if zeroHelpers < 2 {
		t.Fatalf("expected at least two zero-original helpers, got %d", zeroHelpers)
	}
	if len(amounts) <= 1 {
		t.Fatalf("expected zero-original helpers to have varied amounts, got %v", amounts)
	}
}

func TestTransferRemarksExactMatch(t *testing.T) {
	people := []laborPerson{
		{Name: "A", Original: 80000, Adjusted: 0},
		{Name: "B", Original: 0, Adjusted: 50000},
		{Name: "C", Original: 0, Adjusted: 30000},
	}
	transfers := buildLaborTransferPlan(people, 80000, 80000)
	applyLaborTransferRemarks(people, transfers)

	if people[0].Remarks[0] != "由B代发500元" || people[0].Remarks[1] != "由C代发300元" {
		t.Fatalf("unexpected A remarks: %#v", people[0].Remarks)
	}
	if people[1].Remarks[0] != "帮A代发500元" {
		t.Fatalf("unexpected B remarks: %#v", people[1].Remarks)
	}
	if people[2].Remarks[0] != "帮A代发300元" {
		t.Fatalf("unexpected C remarks: %#v", people[2].Remarks)
	}
}

func TestTransferRemarksBestEffortSplit(t *testing.T) {
	people := []laborPerson{
		{Name: "A", Original: 80000, Adjusted: 0},
		{Name: "B", Original: 20000, Adjusted: 0},
		{Name: "C", Original: 0, Adjusted: 50000},
		{Name: "D", Original: 0, Adjusted: 50000},
	}
	transfers := buildLaborTransferPlan(people, 100000, 100000)
	applyLaborTransferRemarks(people, transfers)

	joinedD := strings.Join(people[3].Remarks, "，")
	if people[2].Remarks[0] != "帮A代发500元" {
		t.Fatalf("unexpected C remarks: %#v", people[2].Remarks)
	}
	if !strings.Contains(joinedD, "帮A代发300元") || !strings.Contains(joinedD, "帮B代发200元") {
		t.Fatalf("unexpected D remarks: %#v", people[3].Remarks)
	}
}

func TestZeroOriginalsAreUsedBeforeLowOriginals(t *testing.T) {
	seed := int64(19)
	result, err := adjustLabor([]laborPerson{
		{Name: "A", Original: 160000},
		{Name: "B", Original: 0},
		{Name: "C", Original: 0},
		{Name: "D", Original: 50000},
	}, 360000, &seed)
	if err != nil {
		t.Fatalf("adjustLabor returned error: %v", err)
	}

	byName := map[string]laborPerson{}
	for _, person := range result.People {
		byName[person.Name] = person
	}
	if byName["B"].Adjusted+byName["C"].Adjusted != 150000 {
		t.Fatalf("zero-original helpers did not absorb available surplus first: B=%d C=%d", byName["B"].Adjusted, byName["C"].Adjusted)
	}
	if byName["D"].Adjusted != byName["D"].Original {
		t.Fatalf("low-original person received surplus before zero-original capacity was exhausted: D=%d", byName["D"].Adjusted)
	}
}

func TestUnchangedPersonRemarkIsBlank(t *testing.T) {
	people := []laborPerson{
		{Name: "A", Original: 80000, Adjusted: 80000},
		{Name: "B", Original: 0, Adjusted: 0},
	}
	transfers := buildLaborTransferPlan(people, 80000, 80000)
	applyLaborTransferRemarks(people, transfers)

	for _, person := range people {
		if len(person.Remarks) != 0 {
			t.Fatalf("expected blank remarks for %s, got %#v", person.Name, person.Remarks)
		}
	}
}

func TestLaborWorkbookUsesNumericValuesInsteadOfFormulas(t *testing.T) {
	content, err := createLaborCalculationWorkbook(laborAdjustmentResult{
		People: []laborPerson{
			{Name: "A", Original: 190000, Adjusted: 190000, DutyHours: 18, WorkOrderHours: 5, Management: 120000},
			{Name: "B", Original: 50000, Adjusted: 50000, DutyHours: 10, WorkOrderHours: 5},
		},
	}, nil)
	if err != nil {
		t.Fatalf("createLaborCalculationWorkbook returned error: %v", err)
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("generated workbook cannot be opened: %v", err)
	}
	defer workbook.Close()

	formulaCells := []string{"D2", "F2", "G2", "B4", "D4", "E4", "F4", "G4"}
	for _, cell := range formulaCells {
		formula, err := workbook.GetCellFormula("Sheet1", cell)
		if err != nil {
			t.Fatalf("GetCellFormula(%s) returned error: %v", cell, err)
		}
		if formula != "" {
			t.Fatalf("expected %s to be numeric value, got formula %q", cell, formula)
		}
	}

	rawOptions := excelize.Options{RawCellValue: true}
	checks := map[string]string{
		"D2": "700",
		"E2": "1200",
		"F2": "1900",
		"G2": "76",
		"D4": "1200",
		"E3": "",
		"E4": "1200",
		"F4": "2400",
		"G4": "96",
	}
	for cell, expected := range checks {
		value, err := workbook.GetCellValue("Sheet1", cell, rawOptions)
		if err != nil {
			t.Fatalf("GetCellValue(%s) returned error: %v", cell, err)
		}
		if value != expected {
			t.Fatalf("cell %s = %q, want %q", cell, value, expected)
		}
	}

	expectedNumberFormats := map[string]string{
		"B2": "0.0;[Red]0.0",
		"C2": "0.0;[Red]0.0",
		"D2": "0;[Red]0",
		"E2": "0;[Red]0",
		"F2": "0;[Red]0",
		"G2": "0.0;[Red]0.0",
	}
	for cell, want := range expectedNumberFormats {
		style, err := cellStyle(workbook, cell)
		if err != nil {
			t.Fatalf("cellStyle(%s) returned error: %v", cell, err)
		}
		if style.CustomNumFmt == nil || *style.CustomNumFmt != want {
			t.Fatalf("cell %s number format = %#v, want %q", cell, style.CustomNumFmt, want)
		}
	}
}

func TestLaborWorkbookUsesChineseHeaders(t *testing.T) {
	content, err := createLaborCalculationWorkbook(laborAdjustmentResult{
		People: []laborPerson{
			{Name: "A", Original: 2500, Adjusted: 2500, DutyHours: 1},
		},
	}, nil)
	if err != nil {
		t.Fatalf("createLaborCalculationWorkbook returned error: %v", err)
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("generated workbook cannot be opened: %v", err)
	}
	defer workbook.Close()

	expected := map[string]string{
		"A1": "",
		"B1": "\u503c\u73ed\u5de5\u65f6",
		"C1": "\u5de5\u5355\u5de5\u65f6",
		"D1": "\u5de5\u65f6\u52b3\u52a1\u8d39\u7528\u603b\u8ba1",
		"E1": "\u9879\u76ee\u7ba1\u7406\u8d39\u7528",
		"F1": "\u5e94\u53d1\u52b3\u52a1",
		"G1": "\u5408\u8ba1\u540e\u7684\u5de5\u65f6\u8ba1\u7b97\uff0825\uff09",
		"A3": "\u5408\u8ba1",
	}
	for cell, want := range expected {
		got, err := workbook.GetCellValue("Sheet1", cell)
		if err != nil {
			t.Fatalf("GetCellValue(%s) returned error: %v", cell, err)
		}
		if got != want {
			t.Fatalf("cell %s = %q, want %q", cell, got, want)
		}
	}

	styleChecks := map[string]struct {
		font string
		size float64
		bold bool
	}{
		"B1": {font: "\u9ed1\u4f53", size: 11, bold: true},
		"A2": {font: "\u7b49\u7ebf", size: 12},
		"B2": {font: "\u7b49\u7ebf", size: 11},
		"A3": {font: "\u7b49\u7ebf", size: 11},
	}
	for cell, want := range styleChecks {
		style, err := cellStyle(workbook, cell)
		if err != nil {
			t.Fatalf("cellStyle(%s) returned error: %v", cell, err)
		}
		if style.Font == nil {
			t.Fatalf("cell %s has nil font", cell)
		}
		if style.Font.Family != want.font || style.Font.Size != want.size || style.Font.Bold != want.bold {
			t.Fatalf("cell %s font = %#v, want family=%q size=%v bold=%v", cell, style.Font, want.font, want.size, want.bold)
		}
	}
}

func TestLaborWorkStudyConversionWorkbookMatchesTemplateShape(t *testing.T) {
	content, err := createLaborWorkStudyConversionWorkbook([]laborPerson{
		{Name: "A", Adjusted: 190000},
		{Name: "B", Adjusted: 50000},
	}, "2026-05")
	if err != nil {
		t.Fatalf("createLaborWorkStudyConversionWorkbook returned error: %v", err)
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("generated workbook cannot be opened: %v", err)
	}
	defer workbook.Close()

	expectedValues := map[string]string{
		"A1": "2026\u5e745\u6708\u4efd\u673a\u623f\u8fd0\u8425\u9879\u76ee\u52b3\u52a1\u8d39(30\u95f4\u673a\u623f)",
		"A2": "\u59d3\u540d",
		"B2": "\u5de5\u4f5c\u65f6\u957f\uff08h\uff09",
		"C2": "\u6d4b\u7b97\u6807\u51c6",
		"D2": "\u5e94\u53d1\u52b3\u52a1\u8d39\uff08\u5143\uff09",
		"A3": "A",
		"B3": "76",
		"C3": "25\u5143/\u5c0f\u65f6",
		"A5": "\u603b\u8ba1",
		"C5": "25\u5143/\u5c0f\u65f6",
	}
	rawOptions := excelize.Options{RawCellValue: true}
	for cell, want := range expectedValues {
		got, err := workbook.GetCellValue("Sheet1", cell, rawOptions)
		if err != nil {
			t.Fatalf("GetCellValue(%s) returned error: %v", cell, err)
		}
		if got != want {
			t.Fatalf("cell %s = %q, want %q", cell, got, want)
		}
	}
	expectedFormulas := map[string]string{
		"D3": "B3*25",
		"B5": "SUM(B3:B4)",
		"D5": `"总计："&SUM(D3:D4)&" 元"`,
	}
	for cell, want := range expectedFormulas {
		got, err := workbook.GetCellFormula("Sheet1", cell)
		if err != nil {
			t.Fatalf("GetCellFormula(%s) returned error: %v", cell, err)
		}
		if got != want {
			t.Fatalf("formula %s = %q, want %q", cell, got, want)
		}
	}
}

func TestSafeLaborStemFallbackIsReadableChinese(t *testing.T) {
	if got, want := safeLaborStem(".xlsx"), "DMS财务统计"; got != want {
		t.Fatalf("safeLaborStem fallback = %q, want %q", got, want)
	}
}

func TestLaborWorkbookNameFillColorsByRole(t *testing.T) {
	content, err := createLaborCalculationWorkbook(laborAdjustmentResult{
		People: []laborPerson{
			{Name: "Owner", Adjusted: 2500},
			{Name: "Leader", Adjusted: 2500},
			{Name: "HR", Adjusted: 2500},
			{Name: "User", Adjusted: 2500},
			{Name: "Finance", Adjusted: 2500},
			{Name: "Unknown", Adjusted: 2500},
		},
	}, map[string]string{
		"Owner":   "OWNER",
		"Leader":  "LEADER",
		"HR":      "HR",
		"User":    "USER",
		"Finance": "FINANCE",
	})
	if err != nil {
		t.Fatalf("createLaborCalculationWorkbook returned error: %v", err)
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("generated workbook cannot be opened: %v", err)
	}
	defer workbook.Close()

	checks := map[string]string{
		"A2": "FFC000",
		"A3": "00B0F0",
		"A4": "00B0F0",
		"A5": "FFFFFF",
		"A6": "FFFFFF",
		"A7": "FFFFFF",
	}
	for cell, expected := range checks {
		actual, err := cellFillColor(workbook, cell)
		if err != nil {
			t.Fatalf("cellFillColor(%s) returned error: %v", cell, err)
		}
		if actual != expected {
			t.Fatalf("cell %s fill = %q, want %q", cell, actual, expected)
		}
	}
}

func cellFillColor(workbook *excelize.File, cell string) (string, error) {
	style, err := cellStyle(workbook, cell)
	if err != nil {
		return "", err
	}
	if len(style.Fill.Color) == 0 {
		return "", nil
	}
	color := strings.ToUpper(style.Fill.Color[0])
	if len(color) > 6 {
		color = color[len(color)-6:]
	}
	return color, nil
}

func cellStyle(workbook *excelize.File, cell string) (*excelize.Style, error) {
	styleID, err := workbook.GetCellStyle("Sheet1", cell)
	if err != nil {
		return nil, err
	}
	return workbook.GetStyle(styleID)
}
