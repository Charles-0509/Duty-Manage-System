package http

import (
	"strings"
	"testing"
)

func TestLaborContentDispositionKeepsChineseFilename(t *testing.T) {
	header := laborContentDisposition("20260527-20260630-财务统计-调整后劳务计算.xlsx")

	if !strings.Contains(header, `filename="20260527-20260630-`) || !strings.Contains(header, `.xlsx"`) {
		t.Fatalf("Content-Disposition ascii fallback is unexpected: %s", header)
	}
	if !strings.Contains(header, "filename*=UTF-8''20260527-20260630-%E8%B4%A2%E5%8A%A1%E7%BB%9F%E8%AE%A1-%E8%B0%83%E6%95%B4%E5%90%8E%E5%8A%B3%E5%8A%A1%E8%AE%A1%E7%AE%97.xlsx") {
		t.Fatalf("Content-Disposition does not include UTF-8 filename*: %s", header)
	}
}
