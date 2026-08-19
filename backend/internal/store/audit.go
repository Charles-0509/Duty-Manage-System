package store

import (
	"strings"
	"time"
	"unicode/utf8"

	"personnel-management-go/internal/types"
)

const (
	auditLogMaxRows         = 20000
	auditLogTrimRows        = 16000
	auditMaxPageSize        = 200
	auditUsernameMaxBytes   = 64
	auditRealNameMaxBytes   = 128
	auditActionMaxBytes     = 512
	auditSemesterIDMaxBytes = 64
	auditIPMaxBytes         = 64
)

// InsertAuditLog records one audit entry in the global control database. Audit
// writes must never break the business request, so callers treat errors as
// best-effort logging.
func (s *Store) InsertAuditLog(entry types.AuditLogEntry) error {
	_, err := s.control.Exec(`
		INSERT INTO audit_logs (created_at, username, real_name, action, status, semester_id, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, time.Now().Format("2006-01-02 15:04:05"),
		truncateUTF8(entry.Username, auditUsernameMaxBytes),
		truncateUTF8(entry.RealName, auditRealNameMaxBytes),
		truncateUTF8(entry.Action, auditActionMaxBytes),
		entry.Status,
		truncateUTF8(entry.SemesterID, auditSemesterIDMaxBytes),
		truncateUTF8(entry.IP, auditIPMaxBytes),
	)
	if err != nil {
		return err
	}
	s.pruneAuditLogs()
	return nil
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && !utf8.RuneStart(value[maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}

func (s *Store) pruneAuditLogs() {
	var count int
	if err := s.control.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&count); err != nil || count <= auditLogMaxRows {
		return
	}
	_, _ = s.control.Exec(`
		DELETE FROM audit_logs WHERE id IN (
			SELECT id FROM audit_logs ORDER BY id DESC LIMIT -1 OFFSET ?
		)
	`, auditLogTrimRows)
}

func (s *Store) ListAuditLogs(page, pageSize int, usernameFilter string) (types.AuditLogListResponse, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > auditMaxPageSize {
		pageSize = 50
	}
	usernameFilter = strings.TrimSpace(usernameFilter)

	where := ""
	args := []any{}
	if usernameFilter != "" {
		where = ` WHERE username LIKE ?`
		args = append(args, "%"+usernameFilter+"%")
	}

	var total int64
	if err := s.control.QueryRow(`SELECT COUNT(*) FROM audit_logs`+where, args...).Scan(&total); err != nil {
		return types.AuditLogListResponse{}, err
	}

	query := `
		SELECT id, created_at, username, real_name, action, status, semester_id, ip
		FROM audit_logs` + where + `
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`
	rows, err := s.control.Query(query, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return types.AuditLogListResponse{}, err
	}
	defer rows.Close()

	items := make([]types.AuditLogItem, 0)
	for rows.Next() {
		var item types.AuditLogItem
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.Username, &item.RealName, &item.Action, &item.Status, &item.SemesterID, &item.IP); err != nil {
			return types.AuditLogListResponse{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return types.AuditLogListResponse{}, err
	}

	return types.AuditLogListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}
