package store

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

type laborPerson struct {
	Name           string
	StudentNumber  string
	Original       int64
	DutyHours      float64
	WorkOrderHours float64
	Management     int64
	Adjusted       int64
	Remarks        []string
}

type laborAdjustmentResult struct{ People []laborPerson }

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
		Font:      &excelize.Font{Family: "黑体", Size: 11, Bold: true, Color: "FF0000"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FFFF00"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	ownerNameStyle, _ := file.NewStyle(laborNameStyle("FFC000"))
	leaderNameStyle, _ := file.NewStyle(laborNameStyle("00B0F0"))
	defaultNameStyle, _ := file.NewStyle(laborNameStyle("FFFFFF"))
	bodyStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "等线", Size: 11, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	hoursStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0;[Red]0.0`),
		Font:         &excelize.Font{Family: "等线", Size: 11, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	integerMoneyStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0;[Red]0`),
		Font:         &excelize.Font{Family: "等线", Size: 11, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	totalHoursStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0;[Red]0.0`),
		Font:         &excelize.Font{Family: "等线", Size: 11, Color: "000000"},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{"5B9BD5"}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	totalStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "等线", Size: 11, Color: "000000"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"5B9BD5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	totalIntegerMoneyStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0;[Red]0`),
		Font:         &excelize.Font{Family: "等线", Size: 11, Color: "000000"},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{"5B9BD5"}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	headers := []string{"", "值班工时", "工单工时", "工时劳务费用总计", "项目管理费用", "应发劳务", "合计后的工时计算（25）"}
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
		Font:      &excelize.Font{Family: "宋体", Size: 22, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    laborCellBorders("000000"),
	})
	tableStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "宋体", Size: 18, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    laborCellBorders("000000"),
	})
	hourStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0;[Red]0.0`),
		Font:         &excelize.Font{Family: "宋体", Size: 18, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:       laborCellBorders("000000"),
	})
	amountStyle, _ := file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPointer(`0.0_ `),
		Font:         &excelize.Font{Family: "宋体", Size: 18, Color: "000000"},
		Alignment:    &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:       laborCellBorders("000000"),
	})

	file.MergeCell(sheet, "A1", "D1")
	file.SetCellValue(sheet, "A1", laborWorkStudyTitle(outputMonth))
	file.SetCellStyle(sheet, "A1", "D1", titleStyle)
	file.SetRowHeight(sheet, 1, 27.5)

	headers := []string{"姓名", "工作时长（h）", "测算标准", "应发劳务费（元）"}
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
		file.SetCellValue(sheet, fmt.Sprintf("C%d", row), "25元/小时")
		file.SetCellValue(sheet, fmt.Sprintf("D%d", row), centsToLaborFloat(person.Adjusted))
		file.SetCellStyle(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), tableStyle)
		file.SetCellStyle(sheet, fmt.Sprintf("B%d", row), fmt.Sprintf("B%d", row), hourStyle)
		file.SetCellStyle(sheet, fmt.Sprintf("C%d", row), fmt.Sprintf("C%d", row), tableStyle)
		file.SetCellStyle(sheet, fmt.Sprintf("D%d", row), fmt.Sprintf("D%d", row), amountStyle)
		file.SetRowHeight(sheet, row, 23)
	}

	totalRow := len(people) + 3
	file.SetCellValue(sheet, fmt.Sprintf("A%d", totalRow), "总计")
	if len(people) > 0 {
		var total int64
		for _, person := range people {
			total += person.Adjusted
		}
		file.SetCellValue(sheet, fmt.Sprintf("B%d", totalRow), centsToLaborFloat(total)/25)
		file.SetCellValue(sheet, fmt.Sprintf("D%d", totalRow), fmt.Sprintf("总计：%.2f 元", centsToLaborFloat(total)))
	} else {
		file.SetCellValue(sheet, fmt.Sprintf("B%d", totalRow), 0)
		file.SetCellValue(sheet, fmt.Sprintf("D%d", totalRow), "总计：0 元")
	}
	file.SetCellValue(sheet, fmt.Sprintf("C%d", totalRow), "25元/小时")
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
	return fmt.Sprintf("%d年%d月份机房运营项目劳务费(30间机房)", month.Year(), int(month.Month()))
}

func laborMonthFilenamePrefix(outputMonth string) string {
	month, err := parseLaborCSVOutputMonth(outputMonth)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d年%02d月", month.Year(), int(month.Month()))
}

func laborBlankZeroCents(amount int64) any {
	if amount == 0 {
		return ""
	}
	return centsToLaborFloat(amount)
}

func laborNameStyle(fillColor string) *excelize.Style {
	return &excelize.Style{
		Font:      &excelize.Font{Family: "等线", Size: 12},
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

func laborReportComponents(person laborPerson, rates RateConfig) (float64, float64, int64) {
	dutyHours := person.DutyHours
	dutyCost := int64(math.Round(dutyHours * float64(rates.DutyCents)))
	if dutyCost > person.Adjusted {
		return float64(person.Adjusted) / float64(rates.DutyCents), 0, 0
	}
	remaining := person.Adjusted - dutyCost
	management := min(person.Management, remaining)
	remaining -= management
	workOrderHours := float64(remaining) / float64(rates.WorkOrderCents)
	return dutyHours, workOrderHours, management
}

func stringPointer(value string) *string {
	return &value
}

func centsToLaborFloat(value int64) float64 {
	return float64(value) / 100
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

func formatLaborMoney(value int64) string { return fmt.Sprintf("%.2f", float64(value)/100) }

func parseLaborCSVOutputMonth(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = time.Now().Format("2006-01")
	}
	month, err := time.Parse("2006-01", value)
	if err != nil {
		return time.Time{}, ErrInvalidDateRange
	}
	return month, nil
}
