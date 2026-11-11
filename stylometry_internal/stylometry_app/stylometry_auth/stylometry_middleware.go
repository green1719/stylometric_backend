package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const verifiedTokenContextKey = "stylometry_verified_token"
const sessionIDContextKey = "stylometry_session_id"

// UserExists проверяет существование пользователя в PostgreSQL.
type UserExists func(uint) (bool, error)

// UserID возвращает проверенный ID из контекста или 0 для гостя.
func UserID(ctx *gin.Context) uint {
	token, exists := VerifiedTokenFromContext(ctx)
	if !exists {
		return 0
	}
	return token.UserID
}

// VerifiedTokenFromContext возвращает claims после проверки middleware.
func VerifiedTokenFromContext(ctx *gin.Context) (VerifiedToken, bool) {
	value, exists := ctx.Get(verifiedTokenContextKey)
	if !exists {
		return VerifiedToken{}, false
	}
	token, ok := value.(VerifiedToken)
	if !ok {
		return VerifiedToken{}, false
	}
	return token, true
}

// SessionIDFromContext возвращает sessionId, если запрос аутентифицирован cookie.
func SessionIDFromContext(ctx *gin.Context) (string, bool) {
	value, exists := ctx.Get(sessionIDContextKey)
	sessionID, ok := value.(string)
	return sessionID, exists && ok && sessionID != ""
}

// RequireSessionOrJWT принимает sessionId cookie. Bearer JWT остаётся совместимым
// с уже существующими клиентами и не влияет на остальные маршруты.
func RequireSessionOrJWT(issuer *Issuer, userExists UserExists, blacklist Blacklist, sessions SessionStore) gin.HandlerFunc {
	return verifySessionOrJWT(issuer, userExists, blacklist, sessions, true)
}

// OptionalSessionOrJWT оставляет просмотр публичным, но распознаёт пользователя
// по sessionId cookie или уже поддерживаемому Bearer JWT.
func OptionalSessionOrJWT(issuer *Issuer, userExists UserExists, blacklist Blacklist, sessions SessionStore) gin.HandlerFunc {
	return verifySessionOrJWT(issuer, userExists, blacklist, sessions, false)
}

func verifySessionOrJWT(issuer *Issuer, userExists UserExists, blacklist Blacklist, sessions SessionStore, required bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		sessionID, cookieErr := ctx.Cookie(SessionCookieName)
		if cookieErr == nil && strings.TrimSpace(sessionID) != "" {
			userID, found, err := sessions.UserID(ctx.Request.Context(), sessionID)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "проверка sessionId в Redis временно недоступна"})
				return
			}
			if !found {
				ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "требуется действительная sessionId cookie"})
				return
			}
			exists, err := userExists(userID)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "ошибка базы данных"})
				return
			}
			if !exists {
				ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "требуется действительная sessionId cookie"})
				return
			}
			ctx.Set(verifiedTokenContextKey, VerifiedToken{UserID: userID})
			ctx.Set(sessionIDContextKey, sessionID)
			ctx.Next()
			return
		}

		verifyJWT(issuer, userExists, blacklist, required)(ctx)
	}
}

// RequireJWT требует действительный Bearer JWT для операции создателя.
func RequireJWT(issuer *Issuer, userExists UserExists, blacklist Blacklist) gin.HandlerFunc {
	return verifyJWT(issuer, userExists, blacklist, true)
}

// OptionalJWT оставляет просмотр публичным, но учитывает действительный токен.
func OptionalJWT(issuer *Issuer, userExists UserExists, blacklist Blacklist) gin.HandlerFunc {
	return verifyJWT(issuer, userExists, blacklist, false)
}

func verifyJWT(issuer *Issuer, userExists UserExists, blacklist Blacklist, required bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		header := ctx.GetHeader("Authorization")
		if header == "" && !required {
			ctx.Next()
			return
		}
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "требуется действительный Bearer JWT"})
			return
		}
		token, err := issuer.Verify(parts[1])
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "требуется действительный Bearer JWT"})
			return
		}
		revoked, err := blacklist.IsRevoked(ctx.Request.Context(), token.JTI)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "проверка отзыва JWT временно недоступна"})
			return
		}
		if revoked {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "требуется действительный Bearer JWT"})
			return
		}
		exists, err := userExists(token.UserID)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "ошибка базы данных"})
			return
		}
		if !exists {
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "требуется действительный Bearer JWT"})
			return
		}
		ctx.Set(verifiedTokenContextKey, token)
		ctx.Next()
	}
}
