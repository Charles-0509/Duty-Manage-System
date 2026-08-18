package http

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
)

const laborMaxUploadBytes int64 = 100 * 1024

func (s *server) handleLaborConvert(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, laborMaxUploadBytes+16*1024)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请上传 DMS 导出的财务统计 Excel"})
		return
	}

	if fileHeader.Size > laborMaxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"message": "上传文件不能超过 100KB"})
		return
	}
	targetTotal, err := store.ParseLaborMoneyToCents(c.PostForm("targetTotal"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无法读取上传文件"})
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, laborMaxUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "无法读取上传文件内容"})
		return
	}

	if int64(len(content)) > laborMaxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"message": "上传文件不能超过 100KB"})
		return
	}

	result, err := s.storeFor(c).ConvertLaborWorkbook(content, fileHeader.Filename, targetTotal, c.PostForm("csvOutputMonth"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleLaborConvertFinanceFiles(c *gin.Context) {
	batches, err := s.storeFor(c).ListFinanceLocalBatches()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载本地财务文件失败"})
		return
	}
	items := make([]types.LaborFinanceFileItem, 0, len(batches))
	for _, batch := range batches {
		items = append(items, types.LaborFinanceFileItem{
			ID:            batch.ID,
			CreatedAt:     batch.CreatedAt,
			StartDate:     batch.StartDate,
			EndDate:       batch.EndDate,
			OutputMonth:   batch.OutputMonth,
			ExcelFilename: batch.ExcelFilename,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *server) handleDeleteLaborConvertFinanceFile(c *gin.Context) {
	err := s.storeFor(c).DeleteFinanceLocalBatch(c.Param("id"))
	if err == nil {
		c.JSON(http.StatusOK, types.MessageResponse{Message: "本地财务文件已删除"})
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"message": "本地财务文件不存在"})
		return
	}
	if errors.Is(err, store.ErrFinanceBatchInUse) {
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
		return
	}
	if errors.Is(err, store.ErrArchivedSemester) {
		c.JSON(http.StatusLocked, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"message": "删除本地财务文件失败"})
}

func (s *server) handleLaborConvertFromFinance(c *gin.Context) {
	var request types.LaborConvertFromFinanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "劳务转换参数错误"})
		return
	}
	targetTotal, err := store.ParseLaborMoneyToCents(request.TargetTotal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	result, err := s.storeFor(c).ConvertLaborFinanceBatch(request.BatchID, targetTotal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *server) handleLaborConvertHistory(c *gin.Context) {
	items, err := s.storeFor(c).ListLaborConversionRuns()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "加载劳务转换历史失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *server) handleLaborConvertHistoryDetail(c *gin.Context) {
	item, err := s.storeFor(c).GetLaborConversionRun(c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		message := "加载劳务转换历史详情失败"
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
			message = "历史记录不存在"
		}
		c.JSON(status, gin.H{"message": message})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *server) handleDeleteLaborConvertHistory(c *gin.Context) {
	if err := s.storeFor(c).DeleteLaborConversionRun(c.Param("id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"message": "历史记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": "删除劳务转换历史失败"})
		return
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "劳务转换历史已删除"})
}

func (s *server) handleDownloadLaborConvertWorkbook(c *gin.Context) {
	filename, content, err := s.storeFor(c).GetLaborConversionWorkbook(c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		message := "下载劳务转换结果失败"
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
			message = "历史记录不存在"
		}
		c.JSON(status, gin.H{"message": message})
		return
	}

	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
}

func (s *server) handleDownloadLaborWorkStudyConversionWorkbook(c *gin.Context) {
	filename, content, err := s.storeFor(c).GetLaborWorkStudyConversionWorkbook(c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		message := "\u4e0b\u8f7d\u52b3\u52a1\u52e4\u52a9\u8f6c\u6362\u8868\u5931\u8d25"
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
			message = "\u5386\u53f2\u8bb0\u5f55\u4e0d\u5b58\u5728\u6216\u65e0\u6cd5\u751f\u6210\u52b3\u52a1\u52e4\u52a9\u8f6c\u6362\u8868"
		}
		c.JSON(status, gin.H{"message": message})
		return
	}

	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", content)
}

func (s *server) handleDownloadLaborConvertCSV(c *gin.Context) {
	filename, content, err := s.storeFor(c).GetLaborConversionCSV(c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		message := "下载劳务转换 CSV 失败"
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
			message = "该历史记录暂无可下载 CSV"
		}
		c.JSON(status, gin.H{"message": message})
		return
	}

	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", content)
}

func (s *server) handleDownloadLaborConvertRecords(c *gin.Context) {
	filename, content, err := s.storeFor(c).GetLaborConversionRecordsZip(c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		message := err.Error()
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
			message = "\u8be5\u5386\u53f2\u8bb0\u5f55\u6682\u65e0\u53ef\u751f\u6210\u7684\u8bb0\u5f55\u8868"
		}
		c.JSON(status, gin.H{"message": message})
		return
	}

	c.Header("Content-Disposition", laborContentDisposition(filename))
	c.Data(http.StatusOK, "application/zip", content)
}

func (s *server) handleManualAdjustLaborConvert(c *gin.Context) {
	var request types.LaborManualAdjustRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "手动调额参数错误"})
		return
	}

	result, err := s.storeFor(c).ManualAdjustLaborConversionRun(c.Param("id"), request)
	if err != nil {
		status := http.StatusBadRequest
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func laborContentDisposition(filename string) string {
	asciiName := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, filename)
	asciiName = strings.Trim(asciiName, "-")
	if asciiName == "" {
		asciiName = "labor-convert.xlsx"
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName, url.PathEscape(filename))
}
