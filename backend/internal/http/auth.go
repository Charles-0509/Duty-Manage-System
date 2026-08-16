package http

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"personnel-management-go/internal/http/middleware"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	accessTokenTTL  = 2 * time.Hour
	refreshAttempts = "refresh"
)

func (s *server) loginRateKeys(c *gin.Context, username string) []string {
	return []string{"ip:" + c.ClientIP(), "user:" + strings.ToLower(username)}
}

func (s *server) handleLogin(c *gin.Context) {
	var request types.LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "登录参数不完整"})
		return
	}

	username := strings.TrimSpace(request.Username)
	keys := s.loginRateKeys(c, username)
	for _, key := range keys {
		if allowed, _ := s.loginLimiter.Allow(key); !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{"message": "尝试过于频繁，请稍后再试"})
			return
		}
	}

	user, err := s.store.Authenticate(username, request.Password)
	if err != nil {
		for _, key := range keys {
			s.loginLimiter.RecordFailure(key)
		}
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

	c.JSON(http.StatusOK, types.LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		User:         *user,
	})
}

func (s *server) handleRefreshToken(c *gin.Context) {
	var request types.RefreshTokenRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "刷新参数不完整"})
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
	var request types.RefreshTokenRequest
	if err := c.ShouldBindJSON(&request); err == nil {
		s.storeFor(c).RevokeRefreshToken(strings.TrimSpace(request.RefreshToken))
	}
	c.JSON(http.StatusOK, types.MessageResponse{Message: "已退出登录"})
}

func (s *server) handleChangePassword(c *gin.Context) {
	var request types.ChangePasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "密码参数不完整"})
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
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(accessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   strconv.FormatInt(user.ID, 10),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}
