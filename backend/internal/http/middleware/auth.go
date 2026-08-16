package middleware

import (
	"net/http"
	"strings"
	"time"

	"personnel-management-go/internal/store"
	"personnel-management-go/internal/types"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const userContextKey = "current_user"

type Claims struct {
	UserID int64 `json:"userId"`
	// SessionVersion mirrors accounts.session_version at issue time; bumping
	// the account version invalidates every outstanding access token.
	SessionVersion int64 `json:"sessionVersion"`
	jwt.RegisteredClaims
}

func Auth(secret string, appStore *store.Store) gin.HandlerFunc {
	return authWithResolver(secret, appStore.GetUserByID)
}

func AuthGlobal(secret string, appStore *store.Store) gin.HandlerFunc {
	return authWithResolver(secret, appStore.GetGlobalUserByID)
}

func authWithResolver(secret string, resolve func(int64) (*types.User, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := c.GetHeader("Authorization")
		if authorization == "" || !strings.HasPrefix(authorization, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "缺少登录令牌"})
			c.Abort()
			return
		}

		rawToken := strings.TrimPrefix(authorization, "Bearer ")
		token, err := jwt.ParseWithClaims(rawToken, &Claims{}, func(token *jwt.Token) (any, error) {
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "登录状态已失效"})
			c.Abort()
			return
		}

		claims, ok := token.Claims.(*Claims)
		if !ok || claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(time.Now()) {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "登录状态已失效"})
			c.Abort()
			return
		}

		user, err := resolve(claims.UserID)
		if err != nil || !user.IsActive {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "用户不存在或已停用"})
			c.Abort()
			return
		}
		if claims.SessionVersion != user.SessionVersion {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "登录状态已失效，请重新登录"})
			c.Abort()
			return
		}

		c.Set(userContextKey, *user)
		c.Next()
	}
}

func RequireRoles(roles ...string) gin.HandlerFunc {
	allowed := map[string]struct{}{}
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(c *gin.Context) {
		user := CurrentUser(c)
		if _, ok := allowed[user.Role]; !ok {
			c.JSON(http.StatusForbidden, gin.H{"message": "无权限执行该操作"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func CurrentUser(c *gin.Context) types.User {
	value, _ := c.Get(userContextKey)
	user, _ := value.(types.User)
	return user
}
