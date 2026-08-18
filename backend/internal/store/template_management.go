package store

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"personnel-management-go/internal/types"
)

const (
	workStudyGlobalTemplateFilename   = "勤工助学学生工作记录表模板.docx"
	workStudyStudentNumberPlaceholder = "{{学生学号}}"
	workStudyNamePlaceholder          = "{{姓名}}"
)

func (s *Store) SaveWorkStudyTemplate(content []byte) (types.WorkStudyTemplateItem, error) {
	normalized, err := normalizeGlobalWorkStudyTemplate(content)
	if err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	dir, path := s.globalWorkStudyTemplatePath()
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
	return s.WorkStudyTemplateStatus()
}

func (s *Store) GetWorkStudyTemplate() (string, []byte, error) {
	_, path := s.globalWorkStudyTemplatePath()
	content, err := os.ReadFile(path)
	return workStudyGlobalTemplateFilename, content, err
}

func (s *Store) DeleteWorkStudyTemplate() error {
	_, path := s.globalWorkStudyTemplatePath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) globalWorkStudyTemplatePath() (string, string) {
	dir := s.cfg.WorkStudyTemplateDir
	return dir, filepath.Join(dir, workStudyGlobalTemplateFilename)
}

func (s *Store) WorkStudyTemplateStatus() (types.WorkStudyTemplateItem, error) {
	dir, path := s.globalWorkStudyTemplatePath()
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
