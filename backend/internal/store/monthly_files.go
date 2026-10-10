package store

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

func monthlyOutputNames(month string) (map[string]string, error) {
	date, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, err
	}
	prefix := date.Format("2006年01月")
	return map[string]string{
		"labor":      prefix + "劳务计算.xlsx",
		"work-study": prefix + "劳务勤助转换.xlsx",
		"records":    fmt.Sprintf("%d月勤工助学记录表.zip", date.Month()),
	}, nil
}

// The semester DB owns versioned outputs. This directory is the replaceable
// current-month cache, including the actual Word documents requested by ADMIN.
func (s *Store) monthlyCachePath(reportID string) (string, error) {
	if _, err := uuid.Parse(reportID); err != nil {
		return "", err
	}
	if _, err := uuid.Parse(s.active.ID); err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(s.cfg.SemesterDatabaseDir), "monthly-reports", s.active.ID, reportID), nil
}

func (s *Store) clearMonthlyCache(reportID string) error {
	path, err := s.monthlyCachePath(reportID)
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func (s *Store) restoreMonthlyCache(reportID string) error {
	var status, metadata string
	var labor, workStudy, records []byte
	err := s.db.QueryRow(`SELECT v.status,v.files_json,v.labor_xlsx_blob,v.work_study_xlsx_blob,v.records_zip_blob FROM monthly_reports r JOIN monthly_report_versions v ON v.id=r.latest_version_id WHERE r.id=?`, reportID).Scan(&status, &metadata, &labor, &workStudy, &records)
	if errors.Is(err, sql.ErrNoRows) {
		return s.clearMonthlyCache(reportID)
	}
	if err != nil {
		return err
	}
	if status == "legacy" || len(labor) == 0 || len(workStudy) == 0 || len(records) == 0 {
		return s.clearMonthlyCache(reportID)
	}
	var names map[string]string
	if err = json.Unmarshal([]byte(metadata), &names); err != nil {
		return err
	}
	finish, err := s.replaceMonthlyCache(reportID, names, map[string][]byte{"labor": labor, "work-study": workStudy, "records": records})
	if err != nil {
		return err
	}
	finish(true)
	return nil
}

// Install all three outputs and Word documents as one directory. The caller
// commits the SQLite transaction before releasing the old cached directory.
func (s *Store) replaceMonthlyCache(reportID string, names map[string]string, files map[string][]byte) (func(bool), error) {
	target, err := s.monthlyCachePath(reportID)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(target)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(parent, ".monthly-output-")
	if err != nil {
		return nil, err
	}
	installed := false
	defer func() {
		if !installed {
			os.RemoveAll(staging)
		}
	}()
	for _, kind := range []string{"labor", "work-study", "records"} {
		if err = os.WriteFile(filepath.Join(staging, names[kind]), files[kind], 0600); err != nil {
			return nil, err
		}
	}
	archive, err := zip.NewReader(bytes.NewReader(files["records"]), int64(len(files["records"])))
	if err != nil {
		return nil, err
	}
	wordDir := filepath.Join(staging, "records")
	if err = os.Mkdir(wordDir, 0700); err != nil {
		return nil, err
	}
	for _, entry := range archive.File {
		if entry.Name != filepath.Base(entry.Name) || filepath.Ext(entry.Name) != ".docx" {
			return nil, fmt.Errorf("记录表文件名无效")
		}
		reader, e := entry.Open()
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(reader)
		reader.Close()
		if e != nil {
			return nil, e
		}
		if err = os.WriteFile(filepath.Join(wordDir, entry.Name), data, 0600); err != nil {
			return nil, err
		}
	}
	previous := staging + "-previous"
	oldExists := false
	if _, err = os.Stat(target); err == nil {
		if err = os.Rename(target, previous); err != nil {
			return nil, err
		}
		oldExists = true
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err = os.Rename(staging, target); err != nil {
		if oldExists {
			os.Rename(previous, target)
		}
		return nil, err
	}
	installed = true
	return func(commit bool) {
		if commit {
			if oldExists {
				os.RemoveAll(previous)
			}
			return
		}
		os.RemoveAll(target)
		if oldExists {
			os.Rename(previous, target)
		}
	}, nil
}
