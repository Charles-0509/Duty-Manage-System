package store

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"personnel-management-go/internal/types"
)

const (
	workStudyGlobalTemplateFilename   = "勤工助学学生工作记录表模板.docx"
	workStudyStudentNumberPlaceholder = "{{学生学号}}"
	workStudyNamePlaceholder          = "{{姓名}}"
)

var workStudyStudentNumberPattern = regexp.MustCompile(`[0-9]{6,32}`)

func (s *Store) ListWorkStudyTemplates() ([]types.WorkStudyTemplateItem, error) {
	item, err := s.workStudyTemplateStatus()
	if err != nil {
		return nil, err
	}
	return []types.WorkStudyTemplateItem{item}, nil
}

func (s *Store) SaveWorkStudyTemplate(content []byte) (types.WorkStudyTemplateItem, error) {
	normalized, err := normalizeGlobalWorkStudyTemplate(content)
	if err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	dir, path, err := s.globalWorkStudyTemplatePath()
	if err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	temp, err := os.CreateTemp(dir, ".template-*.docx")
	if err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(normalized); err != nil {
		temp.Close()
		return types.WorkStudyTemplateItem{}, err
	}
	if err := temp.Close(); err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	if err := os.Rename(tempName, path); err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	return s.workStudyTemplateStatus()
}

func (s *Store) GetWorkStudyTemplate() (string, []byte, error) {
	_, path, err := s.globalWorkStudyTemplatePath()
	if err != nil {
		return "", nil, err
	}
	content, err := os.ReadFile(path)
	return workStudyGlobalTemplateFilename, content, err
}

func (s *Store) DeleteWorkStudyTemplate() error {
	_, path, err := s.globalWorkStudyTemplatePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) globalWorkStudyTemplatePath() (string, string, error) {
	dir, err := s.workStudyTemplateDir()
	if err != nil {
		return "", "", err
	}
	return dir, filepath.Join(dir, workStudyGlobalTemplateFilename), nil
}

func (s *Store) workStudyTemplateStatus() (types.WorkStudyTemplateItem, error) {
	dir, path, err := s.globalWorkStudyTemplatePath()
	if err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	item := types.WorkStudyTemplateItem{Filename: workStudyGlobalTemplateFilename}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		item.Exists = true
		item.Size = info.Size()
		item.Updated = info.ModTime().Format("2006-01-02 15:04:05")
	}
	return item, nil
}

func normalizeGlobalWorkStudyTemplate(content []byte) ([]byte, error) {
	return rewriteWorkStudyDOCX(content, func(document []byte) ([]byte, error) {
		if !bytes.Contains(document, []byte(workStudyStudentNumberPlaceholder)) || !bytes.Contains(document, []byte(workStudyNamePlaceholder)) {
			return nil, fmt.Errorf("模板必须包含 %s 和 %s 占位符", workStudyStudentNumberPlaceholder, workStudyNamePlaceholder)
		}
		return clearWorkStudyTemplateDataXML(document)
	})
}

func rewriteWorkStudyDOCX(content []byte, patchDocument func([]byte) ([]byte, error)) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("模板不是有效的 DOCX 文件")
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	documentPatched := false
	for _, file := range reader.File {
		fileContent, err := readZipFile(file)
		if err != nil {
			writer.Close()
			return nil, err
		}
		switch file.Name {
		case "word/document.xml":
			fileContent, err = patchDocument(fileContent)
			if err != nil {
				writer.Close()
				return nil, err
			}
			documentPatched = true
		case "docProps/core.xml":
			fileContent, err = clearWorkStudyCoreProperties(fileContent)
			if err != nil {
				writer.Close()
				return nil, err
			}
		}
		header := file.FileHeader
		header.Name = file.Name
		target, err := writer.CreateHeader(&header)
		if err != nil {
			writer.Close()
			return nil, err
		}
		if _, err := target.Write(fileContent); err != nil {
			writer.Close()
			return nil, err
		}
	}
	if !documentPatched {
		writer.Close()
		return nil, fmt.Errorf("模板缺少 word/document.xml")
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Store) migrateLegacyWorkStudyTemplates() error {
	dir, globalPath, err := s.globalWorkStudyTemplatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type candidate struct {
		name          string
		filename      string
		studentNumber string
		capacity      int
		content       []byte
	}
	studentNumbers := map[string]string{}
	var selected *candidate
	legacySuffix := "_" + workStudyTemplateSuffix
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), legacySuffix) {
			continue
		}
		realName := strings.TrimSuffix(entry.Name(), legacySuffix)
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("读取旧模板 %s 失败: %w", entry.Name(), err)
		}
		document, err := readWorkStudyDocumentXML(content)
		if err != nil {
			return fmt.Errorf("解析旧模板 %s 失败: %w", entry.Name(), err)
		}
		studentNumber, err := extractLegacyWorkStudyStudentNumber(document, realName)
		if err != nil {
			return fmt.Errorf("解析旧模板 %s 失败: %w", entry.Name(), err)
		}
		studentNumbers[realName] = studentNumber
		capacity, err := workStudyTemplateDataCapacity(document)
		if err != nil {
			return fmt.Errorf("解析旧模板 %s 失败: %w", entry.Name(), err)
		}
		if selected == nil || capacity > selected.capacity || capacity == selected.capacity && entry.Name() < selected.filename {
			selected = &candidate{name: realName, filename: entry.Name(), studentNumber: studentNumber, capacity: capacity, content: content}
		}
	}
	seenStudentNumbers := map[string]string{}
	for realName, studentNumber := range studentNumbers {
		if previousName, exists := seenStudentNumbers[studentNumber]; exists && previousName != realName {
			return fmt.Errorf("旧模板中的学号重复：%s 和 %s 使用了 %s", previousName, realName, studentNumber)
		}
		seenStudentNumbers[studentNumber] = realName
	}
	if _, err := os.Stat(globalPath); os.IsNotExist(err) && selected != nil {
		prepared, err := prepareLegacyGlobalWorkStudyTemplate(selected.content, selected.name, selected.studentNumber)
		if err != nil {
			return err
		}
		if _, err := s.SaveWorkStudyTemplate(prepared); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.backfillStudentNumbersAcrossSemesters(studentNumbers)
}

func readWorkStudyDocumentXML(content []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, err
	}
	for _, file := range reader.File {
		if file.Name == "word/document.xml" {
			return readZipFile(file)
		}
	}
	return nil, fmt.Errorf("模板缺少 word/document.xml")
}

func extractLegacyWorkStudyStudentNumber(document []byte, realName string) (string, error) {
	paragraphs, err := findWorkStudyXMLElements(document, "p")
	if err != nil {
		return "", err
	}
	for _, paragraph := range paragraphs {
		text := extractWorkStudyText(document[paragraph.Start:paragraph.End])
		if !strings.Contains(text, "学生学号") || !strings.Contains(text, "姓名") || !strings.Contains(text, realName) {
			continue
		}
		studentNumber := workStudyStudentNumberPattern.FindString(text)
		if err := validateStudentNumber(studentNumber); err == nil && studentNumber != "" {
			return studentNumber, nil
		}
	}
	return "", fmt.Errorf("无法从 %s 的旧模板提取学号", realName)
}

func prepareLegacyGlobalWorkStudyTemplate(content []byte, realName, studentNumber string) ([]byte, error) {
	return rewriteWorkStudyDOCX(content, func(document []byte) ([]byte, error) {
		if !bytes.Contains(document, []byte(realName)) || !bytes.Contains(document, []byte(studentNumber)) {
			return nil, fmt.Errorf("旧模板身份字段无法识别")
		}
		document = bytes.ReplaceAll(document, []byte(studentNumber), []byte(workStudyStudentNumberPlaceholder))
		document = bytes.ReplaceAll(document, []byte(realName), []byte(workStudyNamePlaceholder))
		return clearWorkStudyTemplateDataXML(document)
	})
}

func workStudyTemplateDataCapacity(document []byte) (int, error) {
	table, totalRowIndex, err := locateWorkStudyDataTable(document)
	if err != nil {
		return 0, err
	}
	capacity := totalRowIndex - 2
	if capacity <= 0 || totalRowIndex >= len(table.Rows) {
		return 0, fmt.Errorf("模板数据行区域无效")
	}
	return capacity, nil
}

func locateWorkStudyDataTable(document []byte) (workStudyTable, int, error) {
	tables, err := parseWorkStudyTables(document)
	if err != nil {
		return workStudyTable{}, -1, err
	}
	var selected workStudyTable
	selectedTotalRow := -1
	for _, table := range tables {
		if len(table.Rows) < 3 {
			continue
		}
		if err := validateWorkStudyColumns(detectWorkStudyColumns(table)); err != nil {
			continue
		}
		totalRow := -1
		for rowIndex, row := range table.Rows {
			for _, cell := range row.Cells {
				normalized := strings.Join(strings.Fields(cell.Text), "")
				if strings.Contains(normalized, "合计") || strings.Contains(cell.Text, "合") {
					totalRow = rowIndex
					break
				}
			}
			if totalRow >= 0 {
				break
			}
		}
		if totalRow <= 2 {
			continue
		}
		if selectedTotalRow < 0 || totalRow-2 > selectedTotalRow-2 {
			selected = table
			selectedTotalRow = totalRow
		}
	}
	if selectedTotalRow < 0 {
		return workStudyTable{}, -1, fmt.Errorf("模板中没有可识别的勤工助学记录表")
	}
	return selected, selectedTotalRow, nil
}

func clearWorkStudyTemplateDataXML(document []byte) ([]byte, error) {
	table, totalRowIndex, err := locateWorkStudyDataTable(document)
	if err != nil {
		return nil, err
	}
	replacements := map[string]workStudyReplacement{}
	for rowIndex := 2; rowIndex < totalRowIndex; rowIndex++ {
		for _, cell := range table.Rows[rowIndex].Cells {
			addWorkStudyCellReplacement(replacements, document, cell, "", workStudyTextStyleDefault)
		}
	}
	totalCell, ok := findWorkStudyTotalHoursCell(table.Rows[totalRowIndex])
	if !ok {
		return nil, fmt.Errorf("模板合计行缺少总时数单元格")
	}
	addWorkStudyCellReplacement(replacements, document, totalCell, "小时", workStudyTextStyleTotalHours)
	return applyWorkStudyReplacements(document, replacements), nil
}

func (s *Store) backfillStudentNumbersAcrossSemesters(studentNumbers map[string]string) error {
	rows, err := s.control.Query(`
		SELECT id, name, database_filename, first_monday, archived, draft, active
		FROM semesters ORDER BY created_at
	`)
	if err != nil {
		return err
	}
	type semesterDatabase struct {
		item     types.SemesterSummary
		filename string
	}
	items := make([]semesterDatabase, 0)
	for rows.Next() {
		var item semesterDatabase
		var archived, draft, active int
		if err := rows.Scan(&item.item.ID, &item.item.Name, &item.filename, &item.item.FirstMonday, &archived, &draft, &active); err != nil {
			rows.Close()
			return err
		}
		item.item.Archived = archived == 1
		item.item.Draft = draft == 1
		item.item.Active = active == 1
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].item.ID < items[j].item.ID })
	for _, item := range items {
		db := s.db
		closeDB := false
		if item.item.ID != s.active.ID {
			db, err = sql.Open("sqlite", filepath.Join(s.cfg.SemesterDatabaseDir, item.filename))
			if err != nil {
				return err
			}
			closeDB = true
			if err := configureSQLite(db); err != nil {
				db.Close()
				return err
			}
		}
		temp := &Store{db: db, control: s.control, cfg: s.cfg, active: item.item}
		if err := temp.initSchema(); err != nil {
			if closeDB {
				db.Close()
			}
			return err
		}
		if err := temp.ensureSemesterSchema(); err != nil {
			if closeDB {
				db.Close()
			}
			return err
		}
		if err := temp.ensureSemesterSettings(); err != nil {
			if closeDB {
				db.Close()
			}
			return err
		}
		for realName, studentNumber := range studentNumbers {
			if _, err := db.Exec(`UPDATE users SET student_number = ? WHERE real_name = ? AND TRIM(student_number) = ''`, studentNumber, realName); err != nil {
				if closeDB {
					db.Close()
				}
				return err
			}
		}
		if err := backfillLaborStudentNumberSnapshots(db, studentNumbers); err != nil {
			if closeDB {
				db.Close()
			}
			return err
		}
		if closeDB {
			if err := db.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func backfillLaborStudentNumberSnapshots(db *sql.DB, studentNumbers map[string]string) error {
	rows, err := db.Query(`SELECT id, people_json FROM labor_conversion_runs WHERE TRIM(people_json) != ''`)
	if err != nil {
		return err
	}
	type snapshotUpdate struct {
		id      string
		payload string
	}
	updates := make([]snapshotUpdate, 0)
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			rows.Close()
			return err
		}
		var people []laborPerson
		if err := json.Unmarshal([]byte(payload), &people); err != nil {
			rows.Close()
			return fmt.Errorf("劳务历史 %s 的人员快照无法读取: %w", id, err)
		}
		changed := false
		for index := range people {
			if strings.TrimSpace(people[index].StudentNumber) != "" {
				continue
			}
			studentNumber := studentNumbers[strings.TrimSpace(people[index].Name)]
			if studentNumber == "" {
				continue
			}
			people[index].StudentNumber = studentNumber
			changed = true
		}
		if !changed {
			continue
		}
		updated, err := json.Marshal(people)
		if err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, snapshotUpdate{id: id, payload: string(updated)})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, update := range updates {
		if _, err := tx.Exec(`UPDATE labor_conversion_runs SET people_json = ? WHERE id = ?`, update.payload, update.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
