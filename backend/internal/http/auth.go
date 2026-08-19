package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"personnel-management-go/internal/config"
	"personnel-management-go/internal/http/middleware"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	refreshAttempts           = "refresh"
	authRequestMaxBytes int64 = 8 * 1024
)

func bindAuthJSON(c *gin.Context, request any, message string) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, authRequestMaxBytes)
	if err := c.ShouldBindJSON(request); err != nil {
		status := http.StatusBadRequest
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"message": message})
		return false
	}
	return true
}

// accessTokenTTL returns the configured access-token lifetime; the env knob
// mainly exists so deployments and tests can shorten it.
func (s *server) accessTokenTTL() time.Duration {
	seconds := s.cfg.AccessTokenTTLSeconds
	if seconds <= 0 {
		seconds = 7200
	}
	return time.Duration(seconds) * time.Second
}

func (s *server) loginRateKeys(c *gin.Context, username string) []string {
	return []string{"ip:" + c.ClientIP(), "user:" + strings.ToLower(username)}
}

func (s *server) handleLogin(c *gin.Context) {
	var request types.LoginRequest
	if !bindAuthJSON(c, &request, "登录参数不完整或超过大小限制") {
		return
	}

	username := strings.TrimSpace(request.Username)
	if username == "" || len(username) > config.UsernameMaxBytes || request.Password == "" || len(request.Password) > config.PasswordMaxBytes {
		s.auditLogin(c, "", "", "登录参数非法", http.StatusBadRequest)
		c.JSON(http.StatusBadRequest, gin.H{"message": "用户名或密码格式错误"})
		return
	}
	keys := s.loginRateKeys(c, username)
	for _, key := range keys {
		if allowed, _ := s.loginLimiter.Allow(key); !allowed {
			s.auditLogin(c, username, "", "登录被限流拒绝", http.StatusTooManyRequests)
			c.JSON(http.StatusTooManyRequests, gin.H{"message": "尝试过于频繁，请稍后再试"})
			return
		}
	}

	user, err := s.store.Authenticate(username, request.Password)
	if err != nil {
		for _, key := range keys {
			s.loginLimiter.RecordFailure(key)
		}
		s.auditLogin(c, username, "", "登录失败", http.StatusUnauthorized)
		c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
		return
	}

	for _, key := range keys {
		s.loginLimiter.RecordSuccess(key)
	}

	token, err := s.generateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "生成登录令牌失败"})
		return
	}
	refreshToken, err := s.store.IssueRefreshToken(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "生成登录凭证失败"})
		return
	}

	s.auditLogin(c, user.Username, user.RealName, "登录成功", http.StatusOK)
	c.JSON(http.StatusOK, types.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		User:         *user,
	})
}

func (s *server) handleRefreshToken(c *gin.Context) {
	var request types.RefreshTokenRequest
	if !bindAuthJSON(c, &request, "刷新参数不完整或超过大小限制") {
		return
	}

	ipKey := refreshAttempts + ":ip:" + c.ClientIP()
	if allowed, _ := s.loginLimiter.Allow(ipKey); !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{"message": "尝试过于频繁，请稍后再试"})
		return
	}

	accountID, nextRefresh, err := s.store.RotateRefreshToken(strings.TrimSpace(request.RefreshToken))
	if err != nil {
		s.loginLimiter.RecordFailure(ipKey)
		c.JSON(http.StatusUnauthorized, gin.H{"message": "登录状态已失效，请重新登录"})
		return
	}
	s.loginLimiter.RecordSuccess(ipKey)

	user, err := s.store.GetUserByID(accountID)
	if err != nil || !user.IsActive {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "用户不存在或已停用"})
		return
	}

	token, err := s.generateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "生成登录令牌失败"})
		return
	}

	c.JSON(http.StatusOK, types.LoginResponse{
		Token:        token,
		RefreshToken: nextRefresh,
		User:         *user,
	})
}

func (s *server) handleLogout(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, authRequestMaxBytes)
	var request types.RefreshTokenRequest
	if err := c.ShouldBindJSON(&request); err == nil {
		s.storeFor(c).RevokeRefreshToken(strings.TrimSpace(request.RefreshToken))
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "已退出登录"})
}

func (s *server) handleChangePassword(c *gin.Context) {
	var request types.ChangePasswordRequest
	if !bindAuthJSON(c, &request, "密码参数不完整或超过大小限制") {
		return
	}

	if strings.TrimSpace(request.NewPassword) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "新密码不能为空"})
		return
	}

	user := middleware.CurrentUser(c)
	if err := s.storeFor(c).UpdateOwnPassword(user.ID, request.CurrentPassword, request.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	// UpdateOwnPassword bumped the session version and revoked refresh tokens,
	// so hand the client a fresh pair to keep it signed in on this device.
	updatedUser, err := s.storeFor(c).GetUserByID(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "密码已修改，请重新登录"})
		return
	}
	token, err := s.generateToken(updatedUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "密码已修改，请重新登录"})
		return
	}
	refreshToken, err := s.storeFor(c).IssueRefreshToken(updatedUser.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "密码已修改，请重新登录"})
		return
	}

	c.JSON(http.StatusOK, types.ChangePasswordResponse{
		Message:      "密码修改成功",
		User:         *updatedUser,
		Token:        token,
		RefreshToken: refreshToken,
	})
}

func (s *server) generateToken(user *types.User) (string, error) {
	claims := middleware.Claims{
		UserID:         user.ID,
		SessionVersion: user.SessionVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.accessTokenTTL())),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   strconv.FormatInt(user.ID, 10),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

func (s *server) auditLogin(c *gin.Context, username, realName, action string, status int) {
	active := s.store.ActiveSemester()
	_ = s.store.InsertAuditLog(types.AuditLogEntry{
		Username:   username,
		RealName:   realName,
		Action:     action,
		Status:     status,
		SemesterID: active.ID,
		IP:         c.ClientIP(),
	})
}
