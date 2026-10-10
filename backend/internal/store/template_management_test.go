package store

import (
	"bytes"
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
