package store

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSaveGlobalTemplateRequiresBothPlaceholders(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	template, err := rewriteWorkStudyDOCX(buildWorkStudyTemplateDocx(t, 2), func(document []byte) ([]byte, error) {
		return bytes.ReplaceAll(document, []byte(workStudyNamePlaceholder), []byte("静态姓名")), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appStore.SaveWorkStudyTemplate(template); err == nil || !strings.Contains(err.Error(), workStudyNamePlaceholder) {
		t.Fatalf("expected missing name placeholder error, got %v", err)
	}
}

func TestResolveWorkStudyStudentNumbersUsesHistorySnapshot(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	records := map[string][]workStudyRecord{
		"历史姓名": {{Name: "历史姓名"}},
	}
	snapshot, err := json.Marshal([]laborPerson{{Name: "历史姓名", StudentNumber: "202600000999"}})
	if err != nil {
		t.Fatal(err)
	}
	numbers, err := appStore.resolveWorkStudyStudentNumbers(records, string(snapshot))
	if err != nil {
		t.Fatalf("resolveWorkStudyStudentNumbers: %v", err)
	}
	if numbers["历史姓名"] != "202600000999" {
		t.Fatalf("unexpected student numbers: %#v", numbers)
	}
}
