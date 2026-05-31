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
	styleID, err := workbook.GetCellStyle("Sheet1", cell)
	if err != nil {
		return "", err
	}
	style, err := workbook.GetStyle(styleID)
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
