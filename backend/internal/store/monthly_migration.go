package store

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"personnel-management-go/internal/types"
)

type MonthlyMigrationResult struct {
	Database         string   `json:"database"`
	FinanceCount     int      `json:"financeCount"`
	LaborCount       int      `json:"laborCount"`
	GeneratedRecords int      `json:"generatedRecords"`
	Skipped          bool     `json:"skipped"`
	Warnings         []string `json:"warnings"`
}

type legacyFinanceSource struct {
	ID, CreatedAt, StartDate, EndDate, Month, OrderIDs string
	IncludeManagement, ManagementMonths                int
	ExcelName, CSVName                                 string
	Excel, CSV                                         []byte
	ExcelSHA, CSVSHA                                   string
}

type legacyLaborRun struct {
	ID, CreatedAt, InputName, OutputName, CSVName string
	Target, Original, Final, TeamFund, Seed       int64
	Month, SourceID, ParentID                     string
	Manual                                        int
	People, Result                                string
	Workbook, CSV                                 []byte
}

func legacyMonth(value string) string {
	if _, err := time.Parse("2006-01", value); err != nil {
		return ""
	}
	return value
}

// MigrateMonthlyReports transforms only the known version-4 contract. It does
// not consult live personnel data to reconstruct missing historical facts.
func MigrateMonthlyReports(path string, template []byte) (MonthlyMigrationResult, error) {
	result := MonthlyMigrationResult{Database: path, Warnings: []string{}}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return result, err
	}
	defer db.Close()
	if err = configureSQLite(db); err != nil {
		return result, err
	}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return result, err
	}
	if version == 5 {
		result.Skipped = true
		return result, nil
	}
	if version != 4 {
		return result, fmt.Errorf("学期库版本%d不在本次迁移范围", version)
	}
	var integrity string
	if err = db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return result, fmt.Errorf("迁移前数据库完整性检查失败")
	}
	finances := []legacyFinanceSource{}
	rows, err := db.Query(`SELECT id,created_at,start_date,end_date,output_month,work_order_ids_json,include_management,management_months,excel_filename,csv_filename,excel_blob,csv_blob,excel_sha256,csv_sha256 FROM finance_batches ORDER BY created_at,id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var f legacyFinanceSource
		if err = rows.Scan(&f.ID, &f.CreatedAt, &f.StartDate, &f.EndDate, &f.Month, &f.OrderIDs, &f.IncludeManagement, &f.ManagementMonths, &f.ExcelName, &f.CSVName, &f.Excel, &f.CSV, &f.ExcelSHA, &f.CSVSHA); err != nil {
			rows.Close()
			return result, err
		}
		excelHash, csvHash := sha256.Sum256(f.Excel), sha256.Sum256(f.CSV)
		if hex.EncodeToString(excelHash[:]) != f.ExcelSHA || hex.EncodeToString(csvHash[:]) != f.CSVSHA {
			rows.Close()
			return result, fmt.Errorf("财务批次%s的文件哈希校验失败", f.ID)
		}
		finances = append(finances, f)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	runs := []legacyLaborRun{}
	rows, err = db.Query(`SELECT id,created_at,input_filename,output_name,csv_name,target_total_cents,original_total_cents,final_total_cents,team_fund_cents,seed,csv_output_month,source_finance_batch_id,parent_run_id,is_manual_adjust,people_json,result_json,workbook_blob,csv_blob FROM labor_conversion_runs ORDER BY created_at,id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var r legacyLaborRun
		if err = rows.Scan(&r.ID, &r.CreatedAt, &r.InputName, &r.OutputName, &r.CSVName, &r.Target, &r.Original, &r.Final, &r.TeamFund, &r.Seed, &r.Month, &r.SourceID, &r.ParentID, &r.Manual, &r.People, &r.Result, &r.Workbook, &r.CSV); err != nil {
			rows.Close()
			return result, err
		}
		runs = append(runs, r)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	var content, firstMonday string
	if err = db.QueryRow(`SELECT work_study_content,first_monday FROM semester_settings WHERE id=1`).Scan(&content, &firstMonday); err != nil {
		return result, err
	}
	tx, err := db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = initMonthlySchema(tx); err != nil {
		return result, err
	}
	reports := map[string]string{}
	reportFor := func(month string) (string, error) {
		month = legacyMonth(month)
		if id, ok := reports[month]; ok {
			return id, nil
		}
		id := uuid.NewString()
		if _, e := tx.Exec(`INSERT INTO monthly_reports(id,output_month) VALUES(?,?)`, id, month); e != nil {
			return "", e
		}
		reports[month] = id
		return id, nil
	}
	sources := map[string]string{}
	byFinance := map[string]legacyFinanceSource{}
	for _, f := range finances {
		reportID, e := reportFor(f.Month)
		if e != nil {
			return result, e
		}
		var orders []string
		if e = json.Unmarshal([]byte(f.OrderIDs), &orders); e != nil {
			return result, fmt.Errorf("财务批次%s的工单元数据无效", f.ID)
		}
		snapshot := MonthlySnapshot{Conditions: types.MonthlyConditions{OutputMonth: legacyMonth(f.Month), StartDate: f.StartDate, EndDate: f.EndDate, WorkOrderIDs: orders, IncludeManagement: f.IncludeManagement == 1, ManagementMonths: f.ManagementMonths}, FirstMonday: firstMonday, WorkContent: content, Members: []MonthlyMember{}, Duty: []MonthlyDuty{}, WorkOrders: []types.WorkOrder{}}
		hash, data, e := monthlyHash(snapshot)
		if e != nil {
			return result, e
		}
		metadata, _ := json.Marshal(map[string]any{"id": f.ID, "outputMonth": f.Month, "excelName": f.ExcelName, "csvName": f.CSVName, "excelSha256": f.ExcelSHA, "csvSha256": f.CSVSHA, "sourceSnapshotAvailable": false})
		if _, err = tx.Exec(`INSERT INTO monthly_report_sources(id,report_id,snapshot_json,source_hash,finance_xlsx_blob,finance_csv_blob,legacy_metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, f.ID, reportID, string(data), hash, f.Excel, f.CSV, string(metadata), f.CreatedAt); err != nil {
			return result, err
		}
		sources[f.ID] = f.ID
		byFinance[f.ID] = f
	}
	for _, r := range runs {
		reportID, e := reportFor(r.Month)
		if e != nil {
			return result, e
		}
		sourceID := sources[r.SourceID]
		f, hasFinance := byFinance[r.SourceID]
		if r.SourceID != "" && !hasFinance {
			return result, fmt.Errorf("历史%s引用的财务批次不存在", r.ID)
		}
		snapshot := MonthlySnapshot{Conditions: types.MonthlyConditions{OutputMonth: legacyMonth(r.Month)}, FirstMonday: firstMonday, WorkContent: content, Members: []MonthlyMember{}, Duty: []MonthlyDuty{}, WorkOrders: []types.WorkOrder{}}
		if hasFinance {
			snapshot.Conditions.StartDate = f.StartDate
			snapshot.Conditions.EndDate = f.EndDate
		}
		people := []laborPerson{}
		note := "旧历史只读；原Excel/CSV保持不变"
		if strings.TrimSpace(r.People) != "" {
			if e = json.Unmarshal([]byte(r.People), &people); e != nil {
				return result, fmt.Errorf("历史%s人员快照无效", r.ID)
			}
		}
		distribution := MonthlyDistribution{Target: r.Target, Original: r.Original, Declared: r.Final, TeamFund: r.TeamFund, Unallocated: r.Target - r.Final, Rows: []MonthlyAllocatedMember{}, Transfers: []MonthlyTransfer{}}
		for i, p := range people {
			member := MonthlyMember{ID: -int64(i + 1), Name: p.Name, StudentNumber: p.StudentNumber, DutyHours: p.DutyHours, WorkHours: p.WorkOrderHours, Management: p.Management, Original: p.Original}
			snapshot.Members = append(snapshot.Members, member)
			distribution.Rows = append(distribution.Rows, MonthlyAllocatedMember{MonthlyMember: member, Declared: p.Adjusted})
		}
		var savedResult struct {
			Transfers []struct {
				Source, Receiver string
				Amount           string
			}
		}
		if strings.TrimSpace(r.Result) != "" {
			if err = json.Unmarshal([]byte(r.Result), &savedResult); err != nil {
				return result, fmt.Errorf("历史%s代发元数据无效", r.ID)
			}
		}
		for _, transfer := range savedResult.Transfers {
			amount, e := ParseMonthlyMoney(transfer.Amount)
			if e != nil {
				return result, fmt.Errorf("历史%s代发金额无效：%w", r.ID, e)
			}
			kind := "salary"
			if transfer.Source == "团队经费" {
				kind = "team_fund"
			}
			distribution.Transfers = append(distribution.Transfers, MonthlyTransfer{SourceName: transfer.Source, ReceiverName: transfer.Receiver, Kind: kind, Amount: amount})
		}
		if sourceID == "" {
			sourceID = "legacy-upload-" + r.ID
			hash, data, e := monthlyHash(snapshot)
			if e != nil {
				return result, e
			}
			metadata, _ := json.Marshal(map[string]any{"sourceSnapshotAvailable": false, "inputFilename": r.InputName})
			if _, err = tx.Exec(`INSERT INTO monthly_report_sources(id,report_id,snapshot_json,source_hash,legacy_metadata_json,created_at) VALUES(?,?,?,?,?,?)`, sourceID, reportID, string(data), hash, string(metadata), r.CreatedAt); err != nil {
				return result, err
			}
		}
		files := map[string][]byte{"labor": r.Workbook}
		names := map[string]string{"labor": "劳务原件/" + filepath.Base(r.OutputName)}
		if hasFinance {
			files["finance"] = f.Excel
			names["finance"] = "财务原件/" + filepath.Base(f.ExcelName)
			files["finance-csv"] = f.CSV
			names["finance-csv"] = "财务原件/" + filepath.Base(f.CSVName)
		}
		if len(r.CSV) > 0 {
			files["csv"] = r.CSV
			name := filepath.Base(r.CSVName)
			if name == "" || name == "." {
				name = r.ID + ".csv"
			}
			names["csv"] = "劳务原件/" + name
		}
		if len(people) > 0 && legacyMonth(r.Month) != "" {
			converted, e := createLaborWorkStudyConversionWorkbook(people, r.Month)
			if e != nil {
				return result, e
			}
			files["work-study"] = converted
			names["work-study"] = "迁移生成/" + r.Month + "_勤助转换.xlsx"
		}
		recordsAvailable := false
		if len(people) == 0 {
			note += "；缺少人员快照，无法生成记录表"
		} else if len(r.CSV) == 0 {
			note += "；未保存CSV，无法生成记录表"
		} else if len(template) == 0 {
			note += "；全局模板缺失，无法生成记录表"
		} else {
			records, e := parseWorkStudyCSV(r.CSV)
			if e == nil {
				studentNumbers := map[string]string{}
				for _, p := range people {
					studentNumbers[p.Name] = p.StudentNumber
				}
				month := workStudyOutputMonth(r.Month, records)
				var archive []byte
				archive, e = createWorkStudyRecordsZip(records, template, studentNumbers, content, month)
				if e == nil {
					files["records"] = archive
					names["records"] = "迁移生成/" + r.ID + "_勤工助学记录表.zip"
					result.GeneratedRecords++
					recordsAvailable = true
				}
			}
			if e != nil {
				note += "；记录表不可生成：" + e.Error()
			}
		}
		if recordsAvailable {
			note += "；记录表使用原CSV及全局模板于迁移时生成并冻结"
		}
		if legacyMonth(r.Month) == "" {
			note += "；未标月份"
		}
		if !recordsAvailable || legacyMonth(r.Month) == "" {
			result.Warnings = append(result.Warnings, r.ID+": "+note)
		}
		packet, e := monthlyPackage(files, names)
		if e != nil {
			return result, e
		}
		allocation, e := json.Marshal(distribution)
		if e != nil {
			return result, e
		}
		fileNames, _ := json.Marshal(names)
		var number int
		if err = tx.QueryRow(`SELECT COALESCE(MAX(version_number),0)+1 FROM monthly_report_versions WHERE report_id=?`, reportID).Scan(&number); err != nil {
			return result, err
		}
		metadata, _ := json.Marshal(map[string]any{"oldRunId": r.ID, "parentRunId": r.ParentID, "sourceFinanceBatchId": r.SourceID, "seed": r.Seed, "isManualAdjust": r.Manual, "resultJson": r.Result, "peopleJson": r.People})
		// Each legacy version has its own archival view; it never guesses original rates.
		viewSourceID := "legacy-view-" + r.ID
		hash, data, e := monthlyHash(snapshot)
		if e != nil {
			return result, e
		}
		if _, err = tx.Exec(`INSERT INTO monthly_report_sources(id,report_id,snapshot_json,source_hash,legacy_metadata_json,created_at) VALUES(?,?,?,?,?,?)`, viewSourceID, reportID, string(data), hash, string(metadata), r.CreatedAt); err != nil {
			return result, err
		}
		templateHash := ""
		if recordsAvailable {
			sum := sha256.Sum256(template)
			templateHash = hex.EncodeToString(sum[:])
		}
		if _, err = tx.Exec(`INSERT INTO monthly_report_versions(id,report_id,source_id,version_number,parent_version_id,status,allocation_json,target_cents,original_cents,declared_cents,files_json,package_blob,template_hash,legacy_note,created_at) VALUES(?,?,?,?,?,'legacy',?,?,?,?,?,?,?,?,?)`, r.ID, reportID, viewSourceID, number, r.ParentID, string(allocation), r.Target, r.Original, r.Final, string(fileNames), packet, templateHash, note, r.CreatedAt); err != nil {
			return result, err
		}
		if _, err = tx.Exec(`UPDATE monthly_reports SET latest_version_id=?,created_at=MIN(created_at,?),updated_at=? WHERE id=?`, r.ID, r.CreatedAt, r.CreatedAt, reportID); err != nil {
			return result, err
		}
	}
	result.FinanceCount = len(finances)
	result.LaborCount = len(runs)
	// Verify raw files again from the destination before dropping the old tables.
	for _, f := range finances {
		var excel, csv []byte
		if err = tx.QueryRow(`SELECT finance_xlsx_blob,finance_csv_blob FROM monthly_report_sources WHERE id=?`, f.ID).Scan(&excel, &csv); err != nil {
			return result, err
		}
		if sha256.Sum256(excel) != sha256.Sum256(f.Excel) || sha256.Sum256(csv) != sha256.Sum256(f.CSV) {
			return result, fmt.Errorf("迁移后的财务文件校验失败")
		}
	}
	for _, run := range runs {
		var packet []byte
		var metadata string
		if err = tx.QueryRow(`SELECT package_blob,files_json FROM monthly_report_versions WHERE id=?`, run.ID).Scan(&packet, &metadata); err != nil {
			return result, err
		}
		var names map[string]string
		if err = json.Unmarshal([]byte(metadata), &names); err != nil {
			return result, err
		}
		archive, e := zip.NewReader(bytes.NewReader(packet), int64(len(packet)))
		if e != nil {
			return result, e
		}
		for kind, original := range map[string][]byte{"labor": run.Workbook, "csv": run.CSV} {
			if kind == "csv" && len(original) == 0 {
				continue
			}
			found := false
			for _, entry := range archive.File {
				if entry.Name != names[kind] {
					continue
				}
				reader, e := entry.Open()
				if e != nil {
					return result, e
				}
				data, e := io.ReadAll(reader)
				reader.Close()
				if e != nil {
					return result, e
				}
				if sha256.Sum256(data) != sha256.Sum256(original) {
					return result, fmt.Errorf("迁移后的历史%s文件校验失败", run.ID)
				}
				found = true
				break
			}
			if !found {
				return result, fmt.Errorf("迁移后的历史%s缺少原文件", run.ID)
			}
		}
	}
	for _, stmt := range []string{`DROP TABLE labor_conversion_runs`, `DROP TABLE finance_batches`, `UPDATE semester_settings SET schema_version=5`, `PRAGMA user_version=5`} {
		if _, err = tx.Exec(stmt); err != nil {
			return result, err
		}
	}
	if err = tx.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return result, fmt.Errorf("迁移后数据库完整性检查失败")
	}
	foreignKeys, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return result, err
	}
	invalidReference := foreignKeys.Next()
	foreignKeys.Close()
	if invalidReference {
		return result, fmt.Errorf("迁移后数据库引用关系检查失败")
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
