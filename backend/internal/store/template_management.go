package store

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"personnel-management-go/internal/types"
)

func (s *Store) ListWorkStudyTemplates() ([]types.WorkStudyTemplateItem, error) {
	dir, err := s.workStudyTemplateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT real_name FROM users WHERE role != 'ADMIN' AND is_active = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]types.WorkStudyTemplateItem, 0)
	for rows.Next() {
		var realName string
		if err := rows.Scan(&realName); err != nil {
			return nil, err
		}
		filename := fmt.Sprintf("%s_%s", realName, workStudyTemplateSuffix)
		item := types.WorkStudyTemplateItem{RealName: realName, Filename: filename}
		if info, err := os.Stat(filepath.Join(dir, filename)); err == nil && !info.IsDir() {
			item.Exists = true
			item.Size = info.Size()
			item.Updated = info.ModTime().Format("2006-01-02 15:04:05")
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SaveWorkStudyTemplate(memberID int64, content []byte) (types.WorkStudyTemplateItem, error) {
	var realName, role string
	if err := s.db.QueryRow(`SELECT real_name, role FROM users WHERE id = ? AND is_active = 1`, memberID).Scan(&realName, &role); err != nil {
		if err == sql.ErrNoRows {
			return types.WorkStudyTemplateItem{}, fmt.Errorf("成员不存在")
		}
		return types.WorkStudyTemplateItem{}, err
	}
	if role == "ADMIN" {
		return types.WorkStudyTemplateItem{}, fmt.Errorf("系统管理员不需要记录表模板")
	}
	if err := validateDOCX(content); err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	dir, err := s.workStudyTemplateDir()
	if err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	filename := fmt.Sprintf("%s_%s", realName, workStudyTemplateSuffix)
	temp, err := os.CreateTemp(dir, ".template-*.docx")
	if err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return types.WorkStudyTemplateItem{}, err
	}
	if err := temp.Close(); err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	target := filepath.Join(dir, filename)
	if err := os.Rename(tempName, target); err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return types.WorkStudyTemplateItem{}, err
	}
	return types.WorkStudyTemplateItem{RealName: realName, Filename: filename, Exists: true, Size: info.Size(), Updated: info.ModTime().Format("2006-01-02 15:04:05")}, nil
}

func (s *Store) GetWorkStudyTemplate(memberID int64) (string, []byte, error) {
	filename, path, err := s.workStudyTemplatePath(memberID)
	if err != nil {
		return "", nil, err
	}
	content, err := os.ReadFile(path)
	return filename, content, err
}

func (s *Store) DeleteWorkStudyTemplate(memberID int64) error {
	_, path, err := s.workStudyTemplatePath(memberID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) workStudyTemplatePath(memberID int64) (string, string, error) {
	var realName, role string
	if err := s.db.QueryRow(`SELECT real_name, role FROM users WHERE id = ?`, memberID).Scan(&realName, &role); err != nil {
		return "", "", err
	}
	if role == "ADMIN" {
		return "", "", fmt.Errorf("系统管理员没有记录表模板")
	}
	dir, err := s.workStudyTemplateDir()
	if err != nil {
		return "", "", err
	}
	filename := fmt.Sprintf("%s_%s", strings.TrimSpace(realName), workStudyTemplateSuffix)
	target := filepath.Join(dir, filename)
	// Defense in depth: names created before validation existed may contain
	// path separators; never let the resolved path leave the template dir.
	if filepath.Dir(target) != filepath.Clean(dir) {
		return "", "", fmt.Errorf("成员姓名包含非法字符，无法定位模板文件")
	}
	return filename, target, nil
}

func validateDOCX(content []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return fmt.Errorf("模板不是有效的 DOCX 文件")
	}
	for _, file := range reader.File {
		if file.Name == "word/document.xml" {
			return nil
		}
	}
	return fmt.Errorf("模板缺少 word/document.xml")
}
