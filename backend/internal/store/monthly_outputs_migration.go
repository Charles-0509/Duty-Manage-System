package store

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
)

// Version 5 kept an outer ZIP and financial intermediates for each live
// version. Version 6 retains only the three useful outputs; legacy originals
// are deliberately untouched.
func migrateMonthlyOutputs(db *sql.DB, result MonthlyMigrationResult) (MonthlyMigrationResult, error) {
	tx, err := db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	for _, column := range []string{"labor_xlsx_blob", "work_study_xlsx_blob", "records_zip_blob"} {
		if _, err = tx.Exec(`ALTER TABLE monthly_report_versions ADD COLUMN ` + column + ` BLOB`); err != nil {
			return result, err
		}
	}
	type version struct {
		id, month, names string
		packet           []byte
	}
	rows, err := tx.Query(`SELECT v.id,r.output_month,v.files_json,v.package_blob FROM monthly_report_versions v JOIN monthly_reports r ON r.id=v.report_id WHERE v.status!='legacy'`)
	if err != nil {
		return result, err
	}
	versions := []version{}
	for rows.Next() {
		var v version
		if err = rows.Scan(&v.id, &v.month, &v.names, &v.packet); err != nil {
			rows.Close()
			return result, err
		}
		versions = append(versions, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	for _, v := range versions {
		files := map[string][]byte{}
		names := map[string]string{}
		if len(v.packet) > 0 {
			var oldNames map[string]string
			if err = json.Unmarshal([]byte(v.names), &oldNames); err != nil {
				return result, err
			}
			archive, e := zip.NewReader(bytes.NewReader(v.packet), int64(len(v.packet)))
			if e != nil {
				return result, e
			}
			names, err = monthlyOutputNames(v.month)
			if err != nil {
				return result, err
			}
			for _, kind := range []string{"labor", "work-study", "records"} {
				for _, entry := range archive.File {
					if entry.Name == oldNames[kind] {
						reader, e := entry.Open()
						if e != nil {
							return result, e
						}
						data, e := io.ReadAll(reader)
						reader.Close()
						if e != nil {
							return result, e
						}
						files[kind] = data
						break
					}
				}
				if len(files[kind]) == 0 {
					return result, fmt.Errorf("版本%s缺少%s文件，迁移未修改原库", v.id, kind)
				}
			}
		}
		metadata, _ := json.Marshal(names)
		if _, err = tx.Exec(`UPDATE monthly_report_versions SET files_json=?,labor_xlsx_blob=?,work_study_xlsx_blob=?,records_zip_blob=?,package_blob=NULL WHERE id=?`, string(metadata), files["labor"], files["work-study"], files["records"], v.id); err != nil {
			return result, err
		}
	}
	if _, err = tx.Exec(`UPDATE monthly_report_sources SET finance_xlsx_blob=NULL,finance_csv_blob=NULL WHERE legacy_metadata_json=''`); err != nil {
		return result, err
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM monthly_report_sources WHERE legacy_metadata_json!='' AND finance_xlsx_blob IS NOT NULL`).Scan(&result.FinanceCount); err != nil {
		return result, err
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM monthly_report_versions WHERE status='legacy'`).Scan(&result.LaborCount); err != nil {
		return result, err
	}
	for _, query := range []string{`UPDATE semester_settings SET schema_version=6`, `PRAGMA user_version=6`} {
		if _, err = tx.Exec(query); err != nil {
			return result, err
		}
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
